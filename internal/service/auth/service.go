package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
	"golang.org/x/oauth2"
)

const LoginLifetime = 10 * time.Minute
const SessionLifetime = 12 * time.Hour

type Store interface {
	PutOAuthLoginState(context.Context, domain.OAuthLoginState) error
	ConsumeOAuthLoginState(context.Context, string, string, time.Time) (domain.OAuthLoginState, error)
	PutAuthSession(context.Context, string, domain.AuthSession) error
	GetAuthSession(context.Context, string, time.Time) (domain.AuthSession, error)
	DeleteAuthSession(context.Context, string) error
}

type Identity struct {
	ID    int64
	Login string
}

// Provider enables deterministic tests without allowing deployment settings to
// redirect OAuth secrets. The production provider always uses official hosts.
type Provider interface {
	ExchangeIdentity(context.Context, string, string) (Identity, error)
}

type Options struct {
	Now        func() time.Time
	HTTPClient *http.Client
	Provider   Provider
}

type Service struct {
	store        Store
	now          func() time.Time
	origin       string
	allowed      map[int64]struct{}
	publicSignup bool
	oauth        oauth2.Config
	provider     Provider
}

type LoginStart struct {
	AuthorizationURL string
	BrowserBinding   string `json:"-"`
}

type LoginResult struct {
	Token      string `json:"-"`
	ReturnPath string
	Session    domain.AuthSession
}

func New(config Configuration, store Store, options Options) (*Service, error) {
	if config.Validate() != nil || store == nil {
		return nil, ErrConfiguration
	}
	origin, _ := publicOrigin(config.PublicURL)
	if options.Now == nil {
		options.Now = time.Now
	}
	service := &Service{store: store, now: options.Now, origin: origin, allowed: make(map[int64]struct{}, len(config.AllowedUserIDs)), publicSignup: config.AllowPublicSignup}
	for _, id := range config.AllowedUserIDs {
		service.allowed[id] = struct{}{}
	}
	service.oauth = oauth2.Config{
		ClientID: config.ClientID, ClientSecret: config.ClientSecret,
		RedirectURL: origin + "/auth/github/callback",
		Endpoint:    oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams},
		// Empty scopes request only the minimum public identity, not repo/email.
	}
	service.provider = options.Provider
	if service.provider == nil {
		service.provider = newGitHubProvider(service.oauth, options.HTTPClient)
	}
	return service, nil
}

func (service *Service) PublicURL() string         { return service.origin }
func (service *Service) SecureCookies() bool       { return strings.HasPrefix(service.origin, "https://") }
func (service *Service) PublicSignupEnabled() bool { return service.publicSignup }

func (service *Service) Begin(ctx context.Context, returnTo string) (LoginStart, error) {
	returnPath, err := SafeReturnPath(returnTo)
	if err != nil {
		return LoginStart{}, err
	}
	state, err := randomSecret()
	if err != nil {
		return LoginStart{}, ErrStorage
	}
	binding, err := randomSecret()
	if err != nil {
		return LoginStart{}, ErrStorage
	}
	verifier := oauth2.GenerateVerifier()
	now := service.now().UTC()
	row := domain.OAuthLoginState{StateHash: secretHash(state), BindingHash: secretHash(binding), Verifier: verifier, ReturnPath: returnPath, CreatedAt: now, ExpiresAt: now.Add(LoginLifetime)}
	if err := service.store.PutOAuthLoginState(ctx, row); err != nil {
		if errors.Is(err, domain.ErrAuthCapacity) {
			return LoginStart{}, domain.ErrAuthCapacity
		}
		return LoginStart{}, ErrStorage
	}
	return LoginStart{AuthorizationURL: service.oauth.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), BrowserBinding: binding}, nil
}

func (service *Service) Complete(ctx context.Context, state, code, binding string) (LoginResult, error) {
	if !validSecret(state) || !validSecret(binding) || code == "" || len(code) > 2048 || strings.ContainsFunc(code, unicode.IsSpace) || strings.ContainsFunc(code, unicode.IsControl) {
		return LoginResult{}, ErrInvalidState
	}
	now := service.now().UTC()
	row, err := service.store.ConsumeOAuthLoginState(ctx, secretHash(state), secretHash(binding), now)
	if err != nil {
		if errors.Is(err, corestore.ErrNotFound) {
			return LoginResult{}, ErrInvalidState
		}
		return LoginResult{}, ErrStorage
	}
	if row.StateHash != secretHash(state) || row.BindingHash != secretHash(binding) || row.CreatedAt.After(now) || !row.ExpiresAt.After(now) || row.ExpiresAt.Sub(row.CreatedAt) > LoginLifetime || !validVerifier.MatchString(row.Verifier) {
		return LoginResult{}, ErrInvalidState
	}
	returnPath, err := SafeReturnPath(row.ReturnPath)
	if err != nil {
		return LoginResult{}, ErrInvalidState
	}
	requestContext, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	identity, err := service.provider.ExchangeIdentity(requestContext, code, row.Verifier)
	if err != nil || requestContext.Err() != nil || identity.ID <= 0 || !validLogin.MatchString(identity.Login) {
		return LoginResult{}, ErrProvider
	}
	_, admin := service.allowed[identity.ID]
	if !admin && !service.publicSignup {
		return LoginResult{}, ErrForbidden
	}
	token, err := randomSecret()
	if err != nil {
		return LoginResult{}, ErrStorage
	}
	csrf, err := randomSecret()
	if err != nil {
		return LoginResult{}, ErrStorage
	}
	now = service.now().UTC()
	session := domain.AuthSession{GitHubUserID: identity.ID, Login: identity.Login, Admin: admin, CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(SessionLifetime)}
	if err := service.store.PutAuthSession(ctx, secretHash(token), session); err != nil {
		return LoginResult{}, ErrStorage
	}
	return LoginResult{Token: token, ReturnPath: returnPath, Session: session}, nil
}

func (service *Service) Session(ctx context.Context, token string) (domain.AuthSession, error) {
	if !validSecret(token) {
		return domain.AuthSession{}, ErrUnauthenticated
	}
	now := service.now().UTC()
	session, err := service.store.GetAuthSession(ctx, secretHash(token), now)
	if err != nil {
		if errors.Is(err, corestore.ErrNotFound) {
			return domain.AuthSession{}, ErrUnauthenticated
		}
		return domain.AuthSession{}, ErrStorage
	}
	if session.CreatedAt.After(now) || !session.ExpiresAt.After(now) || session.ExpiresAt.Sub(session.CreatedAt) > SessionLifetime || !validSecret(session.CSRFToken) || !validLogin.MatchString(session.Login) {
		return domain.AuthSession{}, ErrUnauthenticated
	}
	_, session.Admin = service.allowed[session.GitHubUserID]
	if !session.Admin && !service.publicSignup {
		_ = service.store.DeleteAuthSession(ctx, secretHash(token))
		return domain.AuthSession{}, ErrForbidden
	}
	return session, nil
}

func (service *Service) Logout(ctx context.Context, token, csrf string) error {
	session, err := service.Session(ctx, token)
	if err != nil {
		return err
	}
	if !validSecret(csrf) || subtle.ConstantTimeCompare([]byte(csrf), []byte(session.CSRFToken)) != 1 {
		return ErrCSRF
	}
	return service.Revoke(ctx, token)
}

// Revoke is used only after a new callback succeeds to rotate a prior session.
func (service *Service) Revoke(ctx context.Context, token string) error {
	if !validSecret(token) {
		return nil
	}
	if err := service.store.DeleteAuthSession(ctx, secretHash(token)); err != nil && !errors.Is(err, corestore.ErrNotFound) {
		return ErrStorage
	}
	return nil
}

var validVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var validLogin = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func randomSecret() (string, error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes[:]), nil
}

func secretHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func validSecret(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'f' || ch >= '0' && ch <= '9') {
			return false
		}
	}
	return true
}

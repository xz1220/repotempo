// Package agentaccess manages read-only, user-owned credentials. The raw SK is
// returned once. SHA256(SK) is the HMAC signing key and remains sensitive.
package agentaccess

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const RepositoriesRead = "repositories:read"
const WatchlistRead = "watchlist:read"
const ClockSkew = 5 * time.Minute

var ErrUnauthenticated = errors.New("agent access: invalid credentials")
var ErrForbidden = errors.New("agent access: scope denied")
var ErrInvalid = errors.New("agent access: invalid input")
var ErrUnavailable = errors.New("agent access: unavailable")

type Store interface {
	PutAgentKey(context.Context, domain.AgentKey) error
	GetAgentKey(context.Context, string) (domain.AgentKey, error)
	ListAgentKeys(context.Context, int64) ([]domain.AgentKey, error)
	RevokeAgentKey(context.Context, int64, string, time.Time) error
	AcceptAgentRequest(context.Context, string, string, time.Time, time.Time) error
}

type Options struct{ Now func() time.Time }
type Service struct {
	store Store
	now   func() time.Time
}
type CreatedKey struct {
	Key    domain.AgentKey
	Secret string `json:"-"`
}

func New(store Store, options Options) (*Service, error) {
	if store == nil {
		return nil, ErrUnavailable
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Service{store: store, now: options.Now}, nil
}

func (s *Service) Create(ctx context.Context, userID int64, name string, scopes []string, days int) (CreatedKey, error) {
	name = strings.TrimSpace(name)
	if userID <= 0 || name == "" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 || strings.ContainsFunc(name, unicode.IsControl) || (days != 30 && days != 90 && days != 365) {
		return CreatedKey{}, ErrInvalid
	}
	if len(scopes) == 0 {
		scopes = []string{RepositoriesRead, WatchlistRead}
	}
	if len(scopes) > 2 {
		return CreatedKey{}, ErrInvalid
	}
	for _, scope := range scopes {
		if scope != RepositoriesRead && scope != WatchlistRead {
			return CreatedKey{}, ErrInvalid
		}
	}
	scopes = slices.Clone(scopes)
	slices.Sort(scopes)
	scopes = slices.Compact(scopes)
	idBytes := make([]byte, 16)
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(idBytes); err != nil {
		return CreatedKey{}, ErrUnavailable
	}
	if _, err := rand.Read(secretBytes); err != nil {
		return CreatedKey{}, ErrUnavailable
	}
	secret := "rt_sk_" + hex.EncodeToString(secretBytes)
	hash := sha256.Sum256([]byte(secret))
	now := s.now().UTC()
	key := domain.AgentKey{ID: "rt_ak_" + hex.EncodeToString(idBytes), UserID: userID, Name: name, Scopes: scopes, SigningKey: hash[:], CreatedAt: now, ExpiresAt: now.Add(time.Duration(days) * 24 * time.Hour)}
	if err := s.store.PutAgentKey(ctx, key); err != nil {
		if errors.Is(err, domain.ErrAgentKeyCapacity) {
			return CreatedKey{}, err
		}
		return CreatedKey{}, ErrUnavailable
	}
	key.SigningKey = nil
	return CreatedKey{Key: key, Secret: secret}, nil
}

func (s *Service) List(ctx context.Context, userID int64) ([]domain.AgentKey, error) {
	if userID <= 0 {
		return nil, ErrUnauthenticated
	}
	keys, err := s.store.ListAgentKeys(ctx, userID)
	if err != nil {
		return nil, ErrUnavailable
	}
	for i := range keys {
		keys[i].SigningKey = nil
	}
	return keys, nil
}
func (s *Service) Revoke(ctx context.Context, userID int64, id string) error {
	if userID <= 0 {
		return ErrUnauthenticated
	}
	err := s.store.RevokeAgentKey(ctx, userID, id, s.now().UTC())
	if errors.Is(err, corestore.ErrNotFound) {
		return ErrInvalid
	}
	if err != nil {
		return ErrUnavailable
	}
	return nil
}
func HasScope(key domain.AgentKey, scope string) bool { return slices.Contains(key.Scopes, scope) }

var keyPattern = regexp.MustCompile(`^rt_ak_[0-9a-f]{32}$`)
var noncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func Sign(secret, method, target, timestamp, nonce string, body []byte) string {
	key := sha256.Sum256([]byte(secret))
	return signKey(key[:], method, target, timestamp, nonce, body)
}
func signKey(key []byte, method, target, timestamp, nonce string, body []byte) string {
	checksum := sha256.Sum256(body)
	canonical := strings.Join([]string{method, target, timestamp, nonce, hex.EncodeToString(checksum[:])}, "\n")
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func (s *Service) Authenticate(ctx context.Context, r *http.Request, body []byte) (domain.AgentKey, error) {
	names := []string{"X-RepoTempo-Key", "X-RepoTempo-Timestamp", "X-RepoTempo-Nonce", "X-RepoTempo-Signature"}
	values := make([]string, 4)
	for i, name := range names {
		headers := r.Header.Values(name)
		if len(headers) != 1 {
			return domain.AgentKey{}, ErrUnauthenticated
		}
		values[i] = headers[0]
	}
	id, stamp, nonce, signature := values[0], values[1], values[2], values[3]
	seconds, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil || strconv.FormatInt(seconds, 10) != stamp || !keyPattern.MatchString(id) || !noncePattern.MatchString(nonce) || len(signature) != 64 {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	sigBytes, err := hex.DecodeString(signature)
	if err != nil {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	now := s.now().UTC()
	requestTime := time.Unix(seconds, 0)
	if requestTime.Before(now.Add(-ClockSkew)) || requestTime.After(now.Add(ClockSkew)) {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	key, err := s.store.GetAgentKey(ctx, id)
	if errors.Is(err, corestore.ErrNotFound) {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	if err != nil {
		return domain.AgentKey{}, ErrUnavailable
	}
	if len(key.SigningKey) != 32 || key.UserID <= 0 || key.Login == "" || key.RevokedAt != nil || key.CreatedAt.After(now) || !key.ExpiresAt.After(now) {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	expected, _ := hex.DecodeString(signKey(key.SigningKey, r.Method, r.URL.RequestURI(), stamp, nonce, body))
	if !hmac.Equal(expected, sigBytes) {
		return domain.AgentKey{}, ErrUnauthenticated
	}
	// A future-dated signature remains valid until requestTime+skew. Retain
	// its nonce until that full window closes, not just now+skew.
	expires := requestTime.Add(ClockSkew + time.Second)
	if rateWindow := now.Add(time.Minute); expires.Before(rateWindow) {
		expires = rateWindow
	}
	if err := s.store.AcceptAgentRequest(ctx, id, nonce, now, expires); err != nil {
		if errors.Is(err, domain.ErrAgentReplay) || errors.Is(err, corestore.ErrNotFound) {
			return domain.AgentKey{}, ErrUnauthenticated
		}
		if errors.Is(err, domain.ErrAgentRateLimited) {
			return domain.AgentKey{}, err
		}
		return domain.AgentKey{}, ErrUnavailable
	}
	key.SigningKey = nil
	return key, nil
}

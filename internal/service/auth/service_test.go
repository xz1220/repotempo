package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

var authNow = time.Date(2026, 9, 10, 3, 0, 0, 0, time.UTC)

type authMemoryStore struct {
	mu       sync.Mutex
	states   map[string]domain.OAuthLoginState
	sessions map[string]domain.AuthSession
	stateErr error
	saveErr  error
}

func newMemoryStore() *authMemoryStore {
	return &authMemoryStore{states: map[string]domain.OAuthLoginState{}, sessions: map[string]domain.AuthSession{}}
}

func (store *authMemoryStore) PutOAuthLoginState(_ context.Context, state domain.OAuthLoginState) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.stateErr != nil {
		return store.stateErr
	}
	store.states[state.StateHash] = state
	return nil
}

func (store *authMemoryStore) ConsumeOAuthLoginState(_ context.Context, stateHash, bindingHash string, now time.Time) (domain.OAuthLoginState, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	state, ok := store.states[stateHash]
	if !ok || state.BindingHash != bindingHash || !state.ExpiresAt.After(now) || state.CreatedAt.After(now) {
		return domain.OAuthLoginState{}, corestore.ErrNotFound
	}
	delete(store.states, stateHash)
	return state, nil
}

func (store *authMemoryStore) PutAuthSession(_ context.Context, hash string, session domain.AuthSession) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.saveErr != nil {
		return store.saveErr
	}
	store.sessions[hash] = session
	return nil
}

func (store *authMemoryStore) GetAuthSession(_ context.Context, hash string, now time.Time) (domain.AuthSession, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, ok := store.sessions[hash]
	if !ok || !value.ExpiresAt.After(now) {
		return domain.AuthSession{}, corestore.ErrNotFound
	}
	return value, nil
}

func (store *authMemoryStore) DeleteAuthSession(_ context.Context, hash string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	delete(store.sessions, hash)
	return nil
}

type providerFunc func(context.Context, string, string) (Identity, error)

func (f providerFunc) ExchangeIdentity(ctx context.Context, code, verifier string) (Identity, error) {
	return f(ctx, code, verifier)
}

func testConfig() Configuration {
	return Configuration{ClientID: "fixture-client", ClientSecret: "fixture-secret", PublicURL: "https://radar.example", AllowedUserIDs: []int64{42}}
}

func testService(t *testing.T, store Store, provider Provider) *Service {
	t.Helper()
	service, err := New(testConfig(), store, Options{Now: func() time.Time { return authNow }, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func beginForTest(t *testing.T, service *Service, path string) (LoginStart, string) {
	t.Helper()
	start, err := service.Begin(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	authorization, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	return start, authorization.Query().Get("state")
}

func TestBeginUsesIndependentBindingAndPKCEMinimalScopes(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, providerFunc(func(context.Context, string, string) (Identity, error) {
		t.Fatal("begin contacted provider")
		return Identity{}, nil
	}))
	path := "/repositories?view=all&new=0&sort=day&cursor=opaque&tag=ai&lang=zh-CN#project-42"
	first, state := beginForTest(t, service, path)
	authorization, _ := url.Parse(first.AuthorizationURL)
	query := authorization.Query()
	if authorization.Scheme != "https" || authorization.Host != "github.com" || authorization.Path != "/login/oauth/authorize" || query.Get("client_id") != "fixture-client" || query.Get("redirect_uri") != "https://radar.example/auth/github/callback" || query.Get("response_type") != "code" || query.Get("code_challenge_method") != "S256" || query.Has("scope") || query.Has("access_type") || query.Has("client_secret") {
		t.Fatalf("unsafe authorization request: %v", query)
	}
	row := store.states[secretHash(state)]
	digest := sha256.Sum256([]byte(row.Verifier))
	if query.Get("code_challenge") != base64.RawURLEncoding.EncodeToString(digest[:]) || row.BindingHash != secretHash(first.BrowserBinding) || row.StateHash == state || row.BindingHash == first.BrowserBinding || state == first.BrowserBinding || !validVerifier.MatchString(row.Verifier) || row.ReturnPath != path || row.ExpiresAt.Sub(row.CreatedAt) != 10*time.Minute {
		t.Fatalf("state/binding/PKCE not independently bound: %+v", row)
	}
	second, secondState := beginForTest(t, service, path)
	if secondState == state || first.BrowserBinding == second.BrowserBinding || store.states[secretHash(secondState)].Verifier == row.Verifier {
		t.Fatal("login attempts reused secrets")
	}
	encoded, _ := json.Marshal(first)
	if strings.Contains(string(encoded), first.BrowserBinding) {
		t.Fatal("browser binding leaked in ordinary JSON")
	}
}

func TestCompleteBindsBrowserConsumesOnceAndStoresOnlySessionHash(t *testing.T) {
	store := newMemoryStore()
	calls := 0
	var expectedVerifier string
	service := testService(t, store, providerFunc(func(ctx context.Context, code, verifier string) (Identity, error) {
		calls++
		if code != "one-use-code" || verifier != expectedVerifier {
			t.Fatal("code exchange lost saved PKCE verifier")
		}
		deadline, ok := ctx.Deadline()
		if !ok || time.Until(deadline) > 12*time.Second {
			t.Fatal("provider has no bounded deadline")
		}
		return Identity{ID: 42, Login: "renamed-owner"}, nil
	}))
	start, state := beginForTest(t, service, "/repositories?new=0")
	expectedVerifier = store.states[secretHash(state)].Verifier
	wrongBinding := strings.Repeat("a", 64)
	if _, err := service.Complete(context.Background(), state, "one-use-code", wrongBinding); !errors.Is(err, ErrInvalidState) || calls != 0 {
		t.Fatalf("cross-browser login accepted: %v calls=%d", err, calls)
	}
	result, err := service.Complete(context.Background(), state, "one-use-code", start.BrowserBinding)
	if err != nil || calls != 1 || result.Session.GitHubUserID != 42 || result.ReturnPath != "/repositories?new=0" || result.Session.Login != "renamed-owner" || !validSecret(result.Token) || !validSecret(result.Session.CSRFToken) || result.Token == result.Session.CSRFToken {
		t.Fatalf("complete failed: %+v %v", result.Session, err)
	}
	if result.Session.ExpiresAt.Sub(result.Session.CreatedAt) != 12*time.Hour || len(store.sessions) != 1 || store.sessions[secretHash(result.Token)].GitHubUserID != 42 {
		t.Fatal("session missing hash or fixed expiry")
	}
	if _, exists := store.sessions[result.Token]; exists {
		t.Fatal("raw cookie token stored in database")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), result.Token) || strings.Contains(string(encoded), result.Session.CSRFToken) {
		t.Fatal("cookie or CSRF token leaked in ordinary JSON")
	}
	if _, err := service.Complete(context.Background(), state, "one-use-code", start.BrowserBinding); !errors.Is(err, ErrInvalidState) || calls != 1 {
		t.Fatalf("replayed state accepted: %v calls=%d", err, calls)
	}
}

func TestDeniedOrFailedProviderNeverCreatesSession(t *testing.T) {
	for name, provider := range map[string]Provider{
		"same-login-wrong-numeric-ID": providerFunc(func(context.Context, string, string) (Identity, error) { return Identity{ID: 999, Login: "owner"}, nil }),
		"provider-error": providerFunc(func(context.Context, string, string) (Identity, error) {
			return Identity{}, errors.New("private secret-access-token URL/config")
		}),
		"missing-ID": providerFunc(func(context.Context, string, string) (Identity, error) { return Identity{Login: "owner"}, nil }),
	} {
		t.Run(name, func(t *testing.T) {
			store := newMemoryStore()
			service := testService(t, store, provider)
			start, state := beginForTest(t, service, "/")
			result, err := service.Complete(context.Background(), state, "code", start.BrowserBinding)
			if err == nil || len(store.sessions) != 0 || result.Token != "" || strings.Contains(err.Error(), "secret-access-token") {
				t.Fatalf("failure created a session or leaked secrets: %+v %v", result, err)
			}
			if name == "same-login-wrong-numeric-ID" && !errors.Is(err, ErrForbidden) {
				t.Fatal("authorization did not use numeric ID")
			}
		})
	}
}

func TestExpiredStateAndMalformedParametersDoNotCallProvider(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, providerFunc(func(context.Context, string, string) (Identity, error) {
		t.Error("invalid state contacted provider")
		return Identity{}, nil
	}))
	start, state := beginForTest(t, service, "/")
	for _, args := range [][3]string{{"", "code", start.BrowserBinding}, {state, "", start.BrowserBinding}, {state, "code", ""}, {state, "code\nsecret", start.BrowserBinding}, {state, strings.Repeat("a", 2049), start.BrowserBinding}} {
		if _, err := service.Complete(context.Background(), args[0], args[1], args[2]); !errors.Is(err, ErrInvalidState) {
			t.Fatalf("invalid callback accepted: %v", err)
		}
	}
	service.now = func() time.Time { return authNow.Add(LoginLifetime) }
	if _, err := service.Complete(context.Background(), state, "code", start.BrowserBinding); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expired state accepted: %v", err)
	}
}

func TestSessionsRecheckWhitelistAndLogoutRequiresCSRF(t *testing.T) {
	store := newMemoryStore()
	config := testConfig()
	service, err := New(config, store, Options{Now: func() time.Time { return authNow }, Provider: providerFunc(func(context.Context, string, string) (Identity, error) { return Identity{ID: 42, Login: "owner"}, nil })})
	if err != nil {
		t.Fatal(err)
	}
	config.AllowedUserIDs[0] = 999 // Caller mutation cannot change the service.
	start, state := beginForTest(t, service, "/")
	result, err := service.Complete(context.Background(), state, "code", start.BrowserBinding)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Session(context.Background(), result.Token); err != nil {
		t.Fatal(err)
	}
	if err := service.Logout(context.Background(), result.Token, strings.Repeat("0", 64)); !errors.Is(err, ErrCSRF) || len(store.sessions) != 1 {
		t.Fatalf("logout bypassed CSRF: %v", err)
	}
	if err := service.Logout(context.Background(), result.Token, result.Session.CSRFToken); err != nil || len(store.sessions) != 0 {
		t.Fatalf("logout failed: %v", err)
	}
	start, state = beginForTest(t, service, "/")
	result, err = service.Complete(context.Background(), state, "new-code", start.BrowserBinding)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := New(config, store, Options{Now: func() time.Time { return authNow }})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.Session(context.Background(), result.Token); !errors.Is(err, ErrForbidden) || len(store.sessions) != 0 {
		t.Fatalf("removed numeric ID retained access: %v", err)
	}
}

func TestSessionExpiryAndExplicitRotation(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, providerFunc(func(context.Context, string, string) (Identity, error) { return Identity{ID: 42, Login: "owner"}, nil }))
	start, state := beginForTest(t, service, "/")
	first, _ := service.Complete(context.Background(), state, "code", start.BrowserBinding)
	start, state = beginForTest(t, service, "/")
	second, _ := service.Complete(context.Background(), state, "code2", start.BrowserBinding)
	if first.Token == second.Token {
		t.Fatal("login reused an existing session token")
	}
	if err := service.Revoke(context.Background(), first.Token); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Session(context.Background(), first.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("old cookie still authorized")
	}
	if _, err := service.Session(context.Background(), second.Token); err != nil {
		t.Fatal("rotation revoked new cookie")
	}
	service.now = func() time.Time { return authNow.Add(SessionLifetime) }
	if _, err := service.Session(context.Background(), second.Token); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired cookie accepted")
	}
}

func TestAuthStorageErrorsAreSanitizedAndCapacityPreserved(t *testing.T) {
	store := newMemoryStore()
	service := testService(t, store, providerFunc(func(context.Context, string, string) (Identity, error) { return Identity{ID: 42, Login: "owner"}, nil }))
	store.stateErr = domain.ErrAuthCapacity
	if _, err := service.Begin(context.Background(), "/"); !errors.Is(err, domain.ErrAuthCapacity) {
		t.Fatal(err)
	}
	store.stateErr = errors.New("database /private/path secret")
	if _, err := service.Begin(context.Background(), "/"); !errors.Is(err, ErrStorage) || strings.Contains(err.Error(), "private") {
		t.Fatal(err)
	}
	store.stateErr = nil
	start, state := beginForTest(t, service, "/")
	store.saveErr = errors.New("database /private/path secret")
	if result, err := service.Complete(context.Background(), state, "code", start.BrowserBinding); !errors.Is(err, ErrStorage) || result.Token != "" || len(store.sessions) != 0 {
		t.Fatal("session persistence error was not handled safely")
	}
}

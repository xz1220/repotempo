package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

type authTransportFunc func(*http.Request) (*http.Response, error)

func (f authTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func providerResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func httpService(t *testing.T, store Store, transport http.RoundTripper) *Service {
	t.Helper()
	service, err := New(testConfig(), store, Options{Now: func() time.Time { return authNow }, HTTPClient: &http.Client{
		Transport: transport, Timeout: time.Hour,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			t.Error("supplied redirect policy must not be used")
			return nil
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestGitHubCodeExchangeUsesOAuthLibraryPKCEAndDiscardsProviderTokens(t *testing.T) {
	store := newMemoryStore()
	calls := 0
	var verifier string
	service := httpService(t, store, authTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.RawQuery != "" {
			t.Fatal("provider URL modified")
		}
		switch r.URL.Host + r.URL.Path {
		case "github.com/login/oauth/access_token":
			if r.Method != http.MethodPost {
				t.Fatal("token exchange is not POST")
			}
			body, _ := io.ReadAll(r.Body)
			form, err := url.ParseQuery(string(body))
			if err != nil || form.Get("client_id") != "fixture-client" || form.Get("client_secret") != "fixture-secret" || form.Get("code") != "authorization-code" || form.Get("code_verifier") != verifier || form.Get("grant_type") != "authorization_code" || form.Get("redirect_uri") != "https://radar.example/auth/github/callback" || form.Has("scope") || form.Has("access_type") {
				t.Fatal("OAuth exchange omitted required fields or requested extra scope")
			}
			return providerResponse(200, `{"access_token":"provider-access-secret","token_type":"bearer","scope":"","refresh_token":"provider-refresh-secret"}`), nil
		case "api.github.com/user":
			if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer provider-access-secret" || r.Header.Get("User-Agent") == "" {
				t.Fatal("user identity was not authenticated")
			}
			return providerResponse(200, `{"id":42,"login":"owner","email":"ignored@example.com"}`), nil
		default:
			t.Fatalf("unexpected provider destination %s", r.URL.Host)
			return nil, ErrProvider
		}
	}))
	start, state := beginForTest(t, service, "/repositories")
	verifier = store.states[secretHash(state)].Verifier
	result, err := service.Complete(context.Background(), state, "authorization-code", start.BrowserBinding)
	if err != nil || calls != 2 || result.Session.GitHubUserID != 42 {
		t.Fatalf("HTTP sign-in failed: %v calls=%d", err, calls)
	}
	for _, value := range []any{result, store.states, store.sessions} {
		encoded, _ := json.Marshal(value)
		if strings.Contains(string(encoded), "provider-access-secret") || strings.Contains(string(encoded), "provider-refresh-secret") || strings.Contains(string(encoded), "ignored@example.com") {
			t.Fatal("provider token/email persisted or exposed")
		}
	}
}

func TestProviderFailuresNeverCreateSessionAndDoNotFollowRedirects(t *testing.T) {
	for _, test := range []struct {
		name, phase string
		status      int
		body        string
	}{
		{"token-redirect", "token", 302, "redirect"}, {"identity-redirect", "user", 302, "redirect"},
		{"token-denied", "token", 400, `{"error":"bad_verification_code","error_description":"private secret"}`},
		{"identity-denied", "user", 401, `{"message":"private secret"}`},
		{"token-unavailable", "token", 503, `{"message":"private secret"}`},
		{"missing-access-token", "token", 200, `{}`}, {"malformed-token", "token", 200, `not json`},
		{"oversized-token", "token", 200, strings.Repeat("a", 65537)}, {"oversized-identity", "user", 200, strings.Repeat("a", 65537)},
		{"missing-ID", "user", 200, `{"login":"owner"}`}, {"string-ID", "user", 200, `{"id":"42","login":"owner"}`},
		{"wrong-ID-same-login", "user", 200, `{"id":999,"login":"owner"}`},
		{"trailing-JSON", "user", 200, `{"id":42,"login":"owner"}{"id":999}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newMemoryStore()
			calls := 0
			service := httpService(t, store, authTransportFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Host != "github.com" && r.URL.Host != "api.github.com" {
					t.Fatal("redirect leaked provider credentials")
				}
				phase := "token"
				if r.URL.Host == "api.github.com" {
					phase = "user"
				}
				if phase == test.phase {
					response := providerResponse(test.status, test.body)
					if test.status == 302 {
						response.Header.Set("Location", "https://evil.invalid/collect")
					}
					return response, nil
				}
				return providerResponse(200, `{"access_token":"ephemeral-token","token_type":"bearer"}`), nil
			}))
			start, state := beginForTest(t, service, "/")
			result, err := service.Complete(context.Background(), state, "code", start.BrowserBinding)
			if err == nil || result.Token != "" || len(store.sessions) != 0 || strings.Contains(err.Error(), "private secret") {
				t.Fatalf("unsafe failure result: %v", err)
			}
			if calls > 2 || test.phase == "token" && calls != 1 {
				t.Fatalf("redirect or retry made extra calls: %d", calls)
			}
			if _, replayErr := service.Complete(context.Background(), state, "code", start.BrowserBinding); !errors.Is(replayErr, ErrInvalidState) {
				t.Fatal("failed exchange did not consume login state")
			}
		})
	}
}

func TestProviderRespectsCancellationWithoutLeakingTransportErrors(t *testing.T) {
	store := newMemoryStore()
	service := httpService(t, store, authTransportFunc(func(r *http.Request) (*http.Response, error) {
		<-r.Context().Done()
		return nil, errors.New("transport secret URL credentials")
	}))
	start, state := beginForTest(t, service, "/")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	before := time.Now()
	if _, err := service.Complete(ctx, state, "code", start.BrowserBinding); !errors.Is(err, ErrProvider) || time.Since(before) > time.Second || len(store.sessions) != 0 {
		t.Fatalf("cancellation was not safe and bounded: %v", err)
	}
}

func TestProviderTransportRejectsNonOfficialCredentialDestinations(t *testing.T) {
	transport := providerTransport{base: authTransportFunc(func(*http.Request) (*http.Response, error) {
		t.Error("unsafe request reached network")
		return nil, ErrProvider
	})}
	for _, address := range []string{"https://evil.invalid/login/oauth/access_token", "http://github.com/login/oauth/access_token", "https://github.com:443/login/oauth/access_token", "https://github.com/login/oauth/access_token?redirect=evil", "https://user:password@github.com/login/oauth/access_token", "https://github.com/login/oauth/%61ccess_token", "https://api.github.com/user"} {
		request, _ := http.NewRequest(http.MethodPost, address, strings.NewReader("client_secret=fixture-secret"))
		if _, err := transport.RoundTrip(request); !errors.Is(err, ErrProvider) {
			t.Fatalf("unsafe endpoint accepted: %s", address)
		}
	}
}

func TestSQLiteIntegrationConsumesStateOnceUnderConcurrentCallbacks(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/auth.db", Now: func() time.Time { return authNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var calls atomic.Int64
	service := testService(t, store, providerFunc(func(context.Context, string, string) (Identity, error) {
		calls.Add(1)
		return Identity{ID: 42, Login: "owner"}, nil
	}))
	start, state := beginForTest(t, service, "/repositories")
	results := make(chan error, 2)
	for range 2 {
		go func() { _, err := service.Complete(ctx, state, "code", start.BrowserBinding); results <- err }()
	}
	first, second := <-results, <-results
	if calls.Load() != 1 || !((first == nil && errors.Is(second, ErrInvalidState)) || (second == nil && errors.Is(first, ErrInvalidState))) {
		t.Fatalf("callback replay race succeeded: calls=%d first=%v second=%v", calls.Load(), first, second)
	}
}

func TestSessionDomainJSONExcludesAuthenticationSecrets(t *testing.T) {
	encoded, _ := json.Marshal(domain.AuthSession{GitHubUserID: 42, Login: "owner", CSRFToken: "hidden-secret"})
	if strings.Contains(string(encoded), "hidden-secret") {
		t.Fatal("domain leaked CSRF")
	}
}

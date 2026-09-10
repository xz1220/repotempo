package web

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	loginauth "github.com/xz1220/repotempo/internal/service/auth"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

const authHTTPOrigin = "https://repotempo.example"
const authHTTPImportID = "12345678-1234-1234-1234-123456789abc"
const authHTTPOldToken = "legacy operator fixture not a real secret"

type authHTTPProvider struct {
	identity         loginauth.Identity
	err              error
	codes, verifiers []string
}

func (provider *authHTTPProvider) ExchangeIdentity(_ context.Context, code, verifier string) (loginauth.Identity, error) {
	provider.codes = append(provider.codes, code)
	provider.verifiers = append(provider.verifiers, verifier)
	return provider.identity, provider.err
}

// Every data method is counted, including optional overview and secondary
// queries. Protected requests must stop before reaching any of these methods.
type authHTTPQueryer struct {
	*fakeQueryer
	calls int
}

func (q *authHTTPQueryer) DashboardSummary(ctx context.Context, at time.Time) (DashboardSummary, error) {
	q.calls++
	return q.fakeQueryer.DashboardSummary(ctx, at)
}
func (q *authHTTPQueryer) ListRepositoryMetrics(ctx context.Context, query RepositoryQuery) (RepositoryPage, error) {
	q.calls++
	return q.fakeQueryer.ListRepositoryMetrics(ctx, query)
}
func (q *authHTTPQueryer) ListRepositoryTrends(ctx context.Context, query RepositoryQuery) (RepositoryPage, error) {
	q.calls++
	return q.fakeQueryer.ListRepositoryTrends(ctx, query)
}
func (q *authHTTPQueryer) GetRepositoryDetail(ctx context.Context, id int64, at time.Time) (RepositoryDetail, error) {
	q.calls++
	return q.fakeQueryer.GetRepositoryDetail(ctx, id, at)
}
func (q *authHTTPQueryer) ListTopicMetrics(ctx context.Context, at time.Time) (TopicPage, error) {
	q.calls++
	return q.fakeQueryer.ListTopicMetrics(ctx, at)
}
func (q *authHTTPQueryer) GetTopicDetail(ctx context.Context, slug string, at time.Time, excluded bool) (TopicDetail, error) {
	q.calls++
	return q.fakeQueryer.GetTopicDetail(ctx, slug, at, excluded)
}
func (q *authHTTPQueryer) DiscoverySummary(ctx context.Context) (DiscoverySummary, error) {
	q.calls++
	return q.fakeQueryer.DiscoverySummary(ctx)
}
func (q *authHTTPQueryer) ListJobRuns(ctx context.Context, limit, offset int) (RunsPage, error) {
	q.calls++
	return q.fakeQueryer.ListJobRuns(ctx, limit, offset)
}
func (q *authHTTPQueryer) Ready(ctx context.Context) error {
	q.calls++
	return q.fakeQueryer.Ready(ctx)
}
func (q *authHTTPQueryer) RadarOverview(ctx context.Context, query RepositoryQuery) (RadarOverview, error) {
	q.calls++
	return q.fakeQueryer.RadarOverview(ctx, query)
}

type authHTTPWatcher struct {
	topics, adds, imports, statuses int
	input                           WatchRequest
}

func (w *authHTTPWatcher) WatchTopics(context.Context) ([]TopicRef, error) {
	w.topics++
	return []TopicRef{}, nil
}
func (w *authHTTPWatcher) AddWatch(_ context.Context, input WatchRequest) (WatchResult, error) {
	w.adds++
	w.input = input
	return WatchResult{ID: 101}, nil
}
func (w *authHTTPWatcher) SubmitImport(_ context.Context, input WatchRequest) (ImportStatus, error) {
	w.imports++
	w.input = input
	return ImportStatus{ID: authHTTPImportID, Repository: input.Repository, Stage: "queued"}, nil
}
func (w *authHTTPWatcher) GetImportStatus(_ context.Context, id string) (ImportStatus, error) {
	w.statuses++
	return ImportStatus{ID: id, Repository: "acme/radar", Stage: "reading"}, nil
}

type authHTTPFixture struct {
	handler  *Handler
	service  *loginauth.Service
	provider *authHTTPProvider
	queryer  *authHTTPQueryer
	watcher  *authHTTPWatcher
	now      time.Time
	logs     bytes.Buffer
}

func newAuthHTTPFixture(t *testing.T) *authHTTPFixture {
	t.Helper()
	fixture := &authHTTPFixture{now: mustTime("2026-09-10T04:00:00Z"), provider: &authHTTPProvider{identity: loginauth.Identity{ID: 42, Login: "site-admin"}}, queryer: &authHTTPQueryer{fakeQueryer: populatedFake()}, watcher: &authHTTPWatcher{}}
	store, err := sqlite.OpenWithConfig(context.Background(), sqlite.Config{Path: ":memory:", Now: func() time.Time { return fixture.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	fixture.service, err = loginauth.New(loginauth.Configuration{ClientID: "fixture-client-id", ClientSecret: "fixture-secret", PublicURL: authHTTPOrigin, AllowedUserIDs: []int64{42}}, store, loginauth.Options{Now: func() time.Time { return fixture.now }, Provider: fixture.provider})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler, err = New(fixture.queryer, Options{Now: func() time.Time { return fixture.now }, Location: domain.ShanghaiLocation(), Locale: localeEnglish, Watcher: fixture.watcher, AllowLocalWrites: true, WriteToken: authHTTPOldToken, Auth: fixture.service, Logger: slog.New(slog.NewTextHandler(&fixture.logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (fixture *authHTTPFixture) request(method, target string, form url.Values, cookies ...*http.Cookie) *http.Request {
	if strings.HasPrefix(target, "/") {
		target = authHTTPOrigin + target
	}
	request := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	request.RemoteAddr = "192.0.2.5:54321"
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method == http.MethodPost {
		request.Header.Set("Origin", authHTTPOrigin)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	return request
}

func (fixture *authHTTPFixture) send(request *http.Request) *httptest.ResponseRecorder {
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	return response
}

func authResponseCookie(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("missing %s cookie (status %d)", name, response.Code)
	return nil
}

func (fixture *authHTTPFixture) begin(t *testing.T, next string) (*url.URL, *http.Cookie) {
	t.Helper()
	response := fixture.send(fixture.request(http.MethodGet, "/auth/github/start?"+url.Values{"next": {next}, "lang": {localeEnglish}}.Encode(), nil))
	if response.Code != http.StatusSeeOther {
		t.Fatalf("start status %d: %s", response.Code, response.Body.String())
	}
	authorization, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return authorization, authResponseCookie(t, response, "__Host-repotempo_oauth")
}

func (fixture *authHTTPFixture) callback(state string, binding *http.Cookie, code string) *httptest.ResponseRecorder {
	query := url.Values{"state": {state}, "code": {code}, "lang": {localeEnglish}}
	return fixture.send(fixture.request(http.MethodGet, "/auth/github/callback?"+query.Encode(), nil, binding))
}

func (fixture *authHTTPFixture) login(t *testing.T) *http.Cookie {
	t.Helper()
	authorization, binding := fixture.begin(t, "/repositories?view=all&new=0&lang=en")
	response := fixture.callback(authorization.Query().Get("state"), binding, "fixture-code")
	if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/repositories?") {
		t.Fatalf("callback failed: %d %s", response.Code, response.Header().Get("Location"))
	}
	return authResponseCookie(t, response, "__Host-repotempo_session")
}

func TestAuthHTTPGitHubFlowUsesIndependentCookiesPKCEAndFixedCallback(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	values := url.Values{"view": {"all"}, "new": {"0"}, "date": {"2026-09-08"}, "lang": {localeChinese}, "q": {"C++ 中文 a+b&c%"}, "cursor": {"2s"}}
	next := "/repositories?" + values.Encode() + "#project-101"
	first, binding := fixture.begin(t, next)
	second, anotherBinding := fixture.begin(t, "/runs")
	for _, authorization := range []*url.URL{first, second} {
		if authorization.Scheme != "https" || authorization.Host != "github.com" || authorization.Path != "/login/oauth/authorize" {
			t.Fatalf("authorization escaped official endpoint: %s", authorization)
		}
		q := authorization.Query()
		if q.Get("redirect_uri") != authHTTPOrigin+"/auth/github/callback" || q.Get("client_id") != "fixture-client-id" || q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || len(q.Get("state")) != 64 || q.Get("scope") != "" || q.Has("client_secret") {
			t.Fatalf("unsafe OAuth request: %s", authorization)
		}
	}
	if first.Query().Get("state") == second.Query().Get("state") || binding.Value == anotherBinding.Value || binding.Value == first.Query().Get("state") {
		t.Fatal("login state/browser bindings are not independent")
	}
	if !binding.Secure || !binding.HttpOnly || binding.SameSite != http.SameSiteLaxMode || binding.Path != "/" || binding.Domain != "" || binding.MaxAge != int(loginauth.LoginLifetime.Seconds()) {
		t.Fatalf("unsafe binding cookie: %+v", binding)
	}
	response := fixture.callback(first.Query().Get("state"), binding, "fixture-code-do-not-leak")
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != next {
		t.Fatalf("callback lost safe scope: %d %s", response.Code, response.Header().Get("Location"))
	}
	cookie := authResponseCookie(t, response, "__Host-repotempo_session")
	if cookie.Value == binding.Value || cookie.Value == first.Query().Get("state") || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != int(loginauth.SessionLifetime.Seconds()) {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	cleared := authResponseCookie(t, response, binding.Name)
	if cleared.MaxAge >= 0 || cleared.Value != "" {
		t.Fatal("one-time browser binding was not cleared")
	}
	if len(fixture.provider.verifiers) != 1 {
		t.Fatal("expected one local identity exchange")
	}
	digest := sha256.Sum256([]byte(fixture.provider.verifiers[0]))
	if base64.RawURLEncoding.EncodeToString(digest[:]) != first.Query().Get("code_challenge") {
		t.Fatal("callback exchange did not use the matching PKCE verifier")
	}
	page := fixture.send(fixture.request(http.MethodGet, "/repositories?new=0", nil, cookie))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "@site-admin") || !strings.Contains(page.Body.String(), `action="/auth/logout`) || !strings.Contains(page.Body.String(), `data-reading-enabled="true"`) {
		t.Fatal("successful local identity did not produce an authenticated header")
	}
}

func TestAuthHTTPRejectsSameLoginWithDifferentImmutableGitHubID(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	fixture.provider.identity.ID = 43
	authorization, binding := fixture.begin(t, "/runs")
	response := fixture.callback(authorization.Query().Get("state"), binding, "private-code")
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "error=forbidden") {
		t.Fatal("same login string bypassed immutable-ID allowlist")
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == "__Host-repotempo_session" && cookie.Value != "" {
			t.Fatal("unlisted ID received a session")
		}
	}
	page := fixture.send(fixture.request(http.MethodGet, response.Header().Get("Location"), nil))
	if !strings.Contains(page.Body.String(), authText(localeEnglish, "forbidden")) || !strings.Contains(page.Body.String(), "github-sign-in") {
		t.Fatal("denied login did not offer a safe retry")
	}
}

func TestAuthHTTPAnonymousOwnerRequestsGateBeforeAnyDataAccess(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, path := range []string{"/watch", "/watch/new", "/watch/imports/" + authHTTPImportID, "/watch/imports/" + authHTTPImportID + "/status", "/runs", "/?focus=1", "/repositories?view=focus", "/repositories?focus=0&focus=1", "/?view=all&view=focus", "/repositories?%66ocus=1", "/repositories?view=focus&focus=0", "/%72uns", "/%77atch/new", "/watch%2Fnew", "/discoveries?focus=1"} {
			t.Run(method+" "+path, func(t *testing.T) {
				response := fixture.send(fixture.request(method, path, nil))
				want := http.StatusSeeOther
				if strings.HasSuffix(path, "/status") {
					want = http.StatusUnauthorized
				}
				if response.Code != want {
					t.Fatalf("status %d, want %d", response.Code, want)
				}
				if fixture.queryer.calls != 0 || fixture.watcher.topics != 0 || fixture.watcher.statuses != 0 || fixture.watcher.imports != 0 {
					t.Fatal("unauthenticated request reached data or import services")
				}
			})
		}
	}
}

func TestAuthHTTPExistingOperatorTokenAndLoopbackNeverBypassOAuth(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	for _, origin := range []string{authHTTPOrigin, "http://127.0.0.1:8878"} {
		form := url.Values{"repository": {"acme/radar"}, "operator_token": {authHTTPOldToken}, "csrf_token": {strings.Repeat("a", 64)}}
		r := fixture.request(http.MethodPost, origin+"/watch", form)
		r.RemoteAddr = "127.0.0.1:1234"
		r.Header.Set("Origin", origin)
		r.AddCookie(&http.Cookie{Name: watchCookie, Value: strings.Repeat("a", 64)})
		response := fixture.send(r)
		if response.Code != http.StatusUnauthorized || fixture.watcher.imports != 0 || fixture.watcher.adds != 0 {
			t.Fatalf("legacy authority bypassed login on %s: %d", origin, response.Code)
		}
	}
}

func TestAuthHTTPSignedInWatchKeepsOriginalCSRFWithoutOperatorToken(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	session := fixture.login(t)
	formResponse := fixture.send(fixture.request(http.MethodGet, "/watch/new", nil, session))
	if formResponse.Code != http.StatusOK || strings.Contains(formResponse.Body.String(), `name="operator_token"`) {
		t.Fatal("authenticated form still requires the old operator token")
	}
	nonce := authResponseCookie(t, formResponse, watchCookie)
	if nonce.SameSite != http.SameSiteStrictMode || nonce.Value == session.Value {
		t.Fatal("watch CSRF and authentication were conflated")
	}
	form := url.Values{"repository": {"acme/radar"}, "focus": {"1"}, "note": {"管理员备注"}}
	invalid := fixture.send(fixture.request(http.MethodPost, "/watch", form, session, nonce))
	if invalid.Code != http.StatusConflict || fixture.watcher.imports != 0 {
		t.Fatal("login bypassed the existing form CSRF check")
	}
	form.Set("csrf_token", nonce.Value)
	valid := fixture.send(fixture.request(http.MethodPost, "/watch", form, session, nonce))
	if valid.Code != http.StatusSeeOther || fixture.watcher.imports != 1 || !fixture.watcher.input.Focus || fixture.watcher.input.Note != "管理员备注" {
		t.Fatalf("authenticated import failed: %d", valid.Code)
	}
	replay := fixture.send(fixture.request(http.MethodPost, "/watch", form, session, nonce))
	if replay.Code != http.StatusConflict || fixture.watcher.imports != 1 {
		t.Fatal("watch nonce was reusable after login")
	}
}

func TestAuthHTTPLogoutRequiresPostOriginAndSessionCSRFThenRevokesReplay(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	cookie := fixture.login(t)
	session, err := fixture.service.Session(context.Background(), cookie.Value)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, method string
		form         url.Values
		change       func(*http.Request)
	}{
		{"get", http.MethodGet, nil, nil},
		{"wrong token", http.MethodPost, url.Values{"csrf_token": {strings.Repeat("f", 64)}}, nil},
		{"duplicate token", http.MethodPost, url.Values{"csrf_token": {session.CSRFToken, session.CSRFToken}}, nil},
		{"external origin", http.MethodPost, url.Values{"csrf_token": {session.CSRFToken}}, func(r *http.Request) { r.Header.Set("Origin", "https://evil.example") }},
		{"missing origin", http.MethodPost, url.Values{"csrf_token": {session.CSRFToken}}, func(r *http.Request) { r.Header.Del("Origin") }},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := fixture.request(test.method, "/auth/logout", test.form, cookie)
			if test.change != nil {
				test.change(r)
			}
			response := fixture.send(r)
			if response.Code < 400 {
				t.Fatalf("invalid logout accepted: %d", response.Code)
			}
			if _, err := fixture.service.Session(context.Background(), cookie.Value); err != nil {
				t.Fatal("invalid logout deleted the valid session")
			}
		})
	}
	valid := fixture.send(fixture.request(http.MethodPost, "/auth/logout?lang=zh-CN", url.Values{"csrf_token": {session.CSRFToken}}, cookie))
	if valid.Code != http.StatusSeeOther || valid.Header().Get("Location") != "/repositories?lang=zh-CN" {
		t.Fatal("valid logout did not return to public browsing")
	}
	cleared := authResponseCookie(t, valid, cookie.Name)
	if cleared.Value != "" || cleared.MaxAge >= 0 || !cleared.HttpOnly || !cleared.Secure {
		t.Fatal("logout did not securely expire the session cookie")
	}
	if _, err := fixture.service.Session(context.Background(), cookie.Value); !errors.Is(err, loginauth.ErrUnauthenticated) {
		t.Fatalf("session replay remains valid: %v", err)
	}
	response := fixture.send(fixture.request(http.MethodGet, "/runs", nil, cookie))
	if response.Code != http.StatusSeeOther || fixture.queryer.calls != 0 {
		t.Fatal("revoked cookie reached private data")
	}
}

func TestAuthHTTPInvalidCallbacksNeverLeakSecretsOrIssueSessions(t *testing.T) {
	for _, kind := range []string{"missing-binding", "wrong-binding", "duplicate-binding", "duplicate-state", "duplicate-code", "malformed", "provider-error", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newAuthHTTPFixture(t)
			authorization, binding := fixture.begin(t, "/runs")
			state := authorization.Query().Get("state")
			code := "secret-code-that-must-not-appear"
			values := url.Values{"state": {state}, "code": {code}}
			cookies := []*http.Cookie{binding}
			switch kind {
			case "missing-binding":
				cookies = nil
			case "wrong-binding":
				cookies = []*http.Cookie{{Name: binding.Name, Value: strings.Repeat("f", 64)}}
			case "duplicate-binding":
				cookies = append(cookies, binding)
			case "duplicate-state":
				values.Add("state", state)
			case "duplicate-code":
				values.Add("code", code)
			case "provider-error":
				fixture.provider.err = errors.New("provider-detail-with-private-access-token")
			case "cancelled":
				values.Set("error", "access_denied")
				values.Set("error_description", "raw-secret-provider-error")
			}
			query := values.Encode()
			if kind == "malformed" {
				query += "&code=%zz"
			}
			response := fixture.send(fixture.request(http.MethodGet, "/auth/github/callback?"+query, nil, cookies...))
			if response.Code != http.StatusSeeOther || !strings.HasPrefix(response.Header().Get("Location"), "/auth/login?error=") {
				t.Fatalf("unsafe callback failure: %d %s", response.Code, response.Header().Get("Location"))
			}
			for _, secret := range []string{state, code, binding.Value, "provider-detail-with-private-access-token", "raw-secret-provider-error"} {
				if strings.Contains(response.Body.String()+response.Header().Get("Location")+fixture.logs.String(), secret) {
					t.Fatal("OAuth failure exposed request/provider secrets")
				}
			}
			for _, cookie := range response.Result().Cookies() {
				if cookie.Name == "__Host-repotempo_session" && cookie.Value != "" {
					t.Fatal("invalid callback issued a session")
				}
			}
			if response.Header().Get("Referrer-Policy") != "no-referrer" || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("OAuth response lacks anti-leak headers")
			}
			if kind != "provider-error" && len(fixture.provider.codes) != 0 {
				t.Fatal("malformed callback reached identity exchange")
			}
			page := fixture.send(fixture.request(http.MethodGet, response.Header().Get("Location"), nil))
			if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "github-sign-in") {
				t.Fatal("callback failure did not allow retry")
			}
		})
	}
}

func TestAuthHTTPStateAndDuplicateSessionCookiesCannotReplay(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	authorization, binding := fixture.begin(t, "/runs")
	state := authorization.Query().Get("state")
	first := fixture.callback(state, binding, "first-code")
	cookie := authResponseCookie(t, first, "__Host-repotempo_session")
	replay := fixture.callback(state, binding, "second-code")
	if !strings.Contains(replay.Header().Get("Location"), "error=invalid") || len(fixture.provider.codes) != 1 {
		t.Fatal("one-time OAuth state was replayed")
	}
	request := fixture.request(http.MethodGet, "/runs", nil, cookie, cookie)
	response := fixture.send(request)
	if response.Code != http.StatusSeeOther || fixture.queryer.calls != 0 {
		t.Fatal("duplicate session cookies supplied administrator authority")
	}
}

func TestAuthHTTPAnonymousProjectionAndOwnerRenderingAreIsolated(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	const privateNote = "owner-only-investment-note"
	fixture.queryer.repositories.Items[0].IsFocus = true
	fixture.queryer.repositories.Items[0].ManualNote = privateNote
	fixture.queryer.repository.Repository.IsFocus = true
	fixture.queryer.repository.Repository.ManualNote = privateNote
	fixture.queryer.repository.Analysis = nil
	for _, path := range []string{"/repositories?new=0", "/repositories/101"} {
		public := fixture.send(fixture.request(http.MethodGet, path, nil))
		body := html.UnescapeString(public.Body.String())
		if public.Code != http.StatusOK || strings.Contains(body, privateNote) || strings.Contains(body, `<span class="new-badge">My watchlist</span>`) || strings.Contains(body, `data-focus-form`) || strings.Contains(body, `class="project-focus-state"`) || !strings.Contains(body, `data-reading-enabled="false"`) {
			t.Fatalf("public page leaks owner state: %s", path)
		}
	}
	cookie := fixture.login(t)
	owner := fixture.send(fixture.request(http.MethodGet, "/repositories/101", nil, cookie))
	if !strings.Contains(owner.Body.String(), privateNote) || !strings.Contains(owner.Body.String(), `<span class="new-badge">My watchlist</span>`) {
		t.Fatal("anonymous requests mutated data later rendered for the owner")
	}
	if !fixture.queryer.repository.Repository.IsFocus || fixture.queryer.repository.Repository.ManualNote != privateNote || !fixture.queryer.repositories.Items[0].IsFocus {
		t.Fatal("shared Queryer data was modified")
	}
}

func TestAuthHTTPUnconfiguredKeepsLegacyAndShowsDisabledLogin(t *testing.T) {
	watcher := &fakeWatcher{}
	handler := watchTestHandler(t, watcher, true, "")
	login := request(t, handler, "/auth/login?lang=en")
	if login.Code != http.StatusServiceUnavailable || !strings.Contains(login.Body.String(), authText(localeEnglish, "unconfigured")) || strings.Contains(login.Body.String(), "github-sign-in") {
		t.Fatal("unconfigured login should be unavailable without locking legacy management")
	}
	nonce := watchFormCookie(t, handler, "http://127.0.0.1:8878")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, watchPOST("http://127.0.0.1:8878", nonce, ""))
	if response.Code != http.StatusSeeOther || watcher.calls != 1 {
		t.Fatal("OAuth absence changed legacy loopback authority")
	}
}

func TestAuthHTTPAliasesCanonicalizeWithoutIssuingCookiesAndProxySpoofsFail(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	next := "/repositories?view=all&new=0&tag=skills"
	alias := fixture.request(http.MethodGet, "https://old-host.example/auth/github/start?"+url.Values{"next": {next}}.Encode(), nil)
	response := fixture.send(alias)
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil || response.Code != http.StatusSeeOther || location.Host != "repotempo.example" || location.Path != "/auth/github/start" || location.Query().Get("next") != next {
		t.Fatal("alias login did not return to the fixed callback origin")
	}
	for _, cookie := range response.Result().Cookies() {
		if strings.HasPrefix(cookie.Name, "__Host-") {
			t.Fatal("alias received an authentication cookie")
		}
	}
	cookie := fixture.login(t)
	spoof := fixture.request(http.MethodGet, "http://repotempo.example/runs", nil, cookie)
	spoof.Header.Set("X-Forwarded-Proto", "https")
	response = fixture.send(spoof)
	if response.Code != http.StatusSeeOther || fixture.queryer.calls != 0 {
		t.Fatal("non-loopback forwarded-proto spoof accepted a privileged cookie")
	}
	proxy := fixture.request(http.MethodGet, "http://repotempo.example/runs", nil, cookie)
	proxy.RemoteAddr = "127.0.0.1:53421"
	proxy.Header.Set("X-Forwarded-Proto", "https")
	if response := fixture.send(proxy); response.Code != http.StatusOK || fixture.queryer.calls != 1 {
		t.Fatal("legitimate local TLS proxy stopped working")
	}
}

func TestAuthHTTPLoginReturnRejectsExternalAndDuplicateDestinations(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	for _, target := range []string{"https://evil.example/", "//evil.example/", "/%72uns", "/repositories?next=https://evil.example"} {
		response := fixture.send(fixture.request(http.MethodGet, "/auth/login?"+url.Values{"next": {target}, "error": {"<script>private-error</script>"}}.Encode(), nil))
		body := html.UnescapeString(response.Body.String())
		if strings.Contains(body, target) || strings.Contains(body, "<script>private-error") {
			t.Fatal("login rendered an untrusted redirect/error")
		}
		match := regexp.MustCompile(`href="([^"]+)"[^>]*>Sign in with GitHub</a>`).FindAllStringSubmatch(response.Body.String(), -1)
		if len(match) == 0 {
			t.Fatal("safe login retry missing")
		}
	}
	duplicate := fixture.send(fixture.request(http.MethodGet, "/auth/github/start?next=%2Fruns&next=%2Frepositories", nil))
	if !strings.Contains(duplicate.Header().Get("Location"), "error=invalid") {
		t.Fatal("duplicate OAuth return paths were accepted")
	}
}

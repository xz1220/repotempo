package web_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/app"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

type agentAPIFixture struct {
	handler *web.Handler
	store   *sqlite.Store
	service *agentaccess.Service
	now     time.Time
	nonce   int
}

type agentPrincipalQueryer struct{ web.Queryer }

func (q agentPrincipalQueryer) ListRepositoryTrends(ctx context.Context, query web.RepositoryQuery) (web.RepositoryPage, error) {
	p, ok := domain.PrincipalFromContext(ctx)
	if !ok || p.UserID <= 0 || p.Admin {
		return web.RepositoryPage{}, fmt.Errorf("API did not supply a non-administrator principal")
	}
	return q.Queryer.ListRepositoryTrends(ctx, query)
}

func newAgentAPIFixture(t *testing.T) *agentAPIFixture {
	t.Helper()
	f := &agentAPIFixture{now: time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC)}
	ctx := context.Background()
	var err error
	f.store, err = sqlite.OpenWithConfig(ctx, sqlite.Config{Path: ":memory:", Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.store.Close() })
	for _, userID := range []int64{42, 99} {
		hash := sha256.Sum256([]byte(fmt.Sprintf("agent API browser fixture %d", userID)))
		if err := f.store.PutAuthSession(ctx, fmt.Sprintf("%x", hash), domain.AuthSession{GitHubUserID: userID, Login: fmt.Sprintf("user-%d", userID), CSRFToken: fmt.Sprintf("%x", hash), CreatedAt: f.now, ExpiresAt: f.now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	for i := int64(1); i <= 14; i++ {
		name := fmt.Sprintf("acme/project-%02d", i)
		description := "Public project description"
		_, _, err := f.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: i, FullName: name, HTMLURL: "https://github.com/" + name, Description: &description, Source: domain.DiscoverySourceGitHubSearch, Profile: "PRIVATE-PROFILE", DiscoveredAt: f.now.Add(-48 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		stars := i * 100
		status := 200
		if _, err := f.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: i, SnapshotDate: domain.ShanghaiDate(f.now), CapturedAt: f.now, StarCount: &stars, FetchStatus: domain.FetchSuccess, HTTPStatus: &status}); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.store.SetUserRepositoryState(ctx, 42, 1, true, "PRIVATE-OWNER-NOTE"); err != nil {
		t.Fatal(err)
	}
	if err := f.store.SetUserRepositoryState(ctx, 99, 2, true, "PRIVATE-OTHER-NOTE"); err != nil {
		t.Fatal(err)
	}
	f.service, err = agentaccess.New(f.store, agentaccess.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = web.New(agentPrincipalQueryer{Queryer: app.WebAdapter{Store: f.store}}, web.Options{Now: func() time.Time { return f.now }, AgentAccess: f.service})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *agentAPIFixture) key(t *testing.T, userID int64, scopes ...string) agentaccess.CreatedKey {
	t.Helper()
	key, err := f.service.Create(context.Background(), userID, "Agent fixture", scopes, 90)
	if err != nil {
		t.Fatal(err)
	}
	return key
}
func (f *agentAPIFixture) request(t *testing.T, key agentaccess.CreatedKey, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	f.nonce++
	nonce := fmt.Sprintf("integration_%016d", f.nonce)
	stamp := strconv.FormatInt(f.now.Unix(), 10)
	r := httptest.NewRequest(method, "https://repotempo.test"+target, strings.NewReader(body))
	r.Header.Set("X-RepoTempo-Key", key.Key.ID)
	r.Header.Set("X-RepoTempo-Timestamp", stamp)
	r.Header.Set("X-RepoTempo-Nonce", nonce)
	r.Header.Set("X-RepoTempo-Signature", agentaccess.Sign(key.Secret, method, r.URL.RequestURI(), stamp, nonce, []byte(body)))
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	for _, private := range []string{key.Secret, "PRIVATE-OWNER-NOTE", "PRIVATE-OTHER-NOTE", "PRIVATE-PROFILE", "signing_key", "SigningKey"} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("response exposed confidential value %q", private)
		}
	}
	return w
}

func TestAgentAPIRealStoreIsolatesWatchlistsAndPaginates(t *testing.T) {
	f := newAgentAPIFixture(t)
	owner := f.key(t, 42)
	other := f.key(t, 99)
	for _, row := range []struct {
		key agentaccess.CreatedKey
		id  int64
	}{{owner, 1}, {other, 2}} {
		w := f.request(t, row.key, "GET", "/api/v1/repositories?view=focus", "")
		if w.Code != 200 {
			t.Fatalf("focus status=%d body=%s", w.Code, w.Body.String())
		}
		var list struct {
			Items []struct {
				ID    int64 `json:"id"`
				Focus bool  `json:"is_focus"`
			} `json:"items"`
			Total int `json:"total"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
			t.Fatal(err)
		}
		if list.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != row.id || !list.Items[0].Focus {
			t.Fatalf("cross-user watchlist result: %s", w.Body.String())
		}
	}
	seen := map[int64]bool{}
	for page := 1; page <= 3; page++ {
		w := f.request(t, owner, "GET", fmt.Sprintf("/api/v1/repositories?size=6&page=%d&sort=name", page), "")
		if w.Code != 200 {
			t.Fatalf("page %d: %s", page, w.Body.String())
		}
		var list struct {
			Items []struct {
				ID int64 `json:"id"`
			} `json:"items"`
			Total   int  `json:"total"`
			HasMore bool `json:"has_more"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &list)
		if list.Total != 14 || list.HasMore != (page < 3) {
			t.Fatal("wrong real pagination totals")
		}
		for _, item := range list.Items {
			if seen[item.ID] {
				t.Fatal("pagination repeated item")
			}
			seen[item.ID] = true
		}
	}
	if len(seen) != 14 {
		t.Fatal("pagination dropped items")
	}
	w := f.request(t, owner, "GET", "/api/v1/repositories/2", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"is_focus":false`) {
		t.Fatalf("detail leaked another user's watch state: %s", w.Body.String())
	}
}

func TestAgentAPIScopesValidationAndMissingCredentials(t *testing.T) {
	f := newAgentAPIFixture(t)
	public := f.key(t, 42, agentaccess.RepositoriesRead)
	watch := f.key(t, 42, agentaccess.WatchlistRead)
	for _, test := range []struct {
		key    agentaccess.CreatedKey
		target string
		status int
	}{{public, "/api/v1/repositories", 200}, {public, "/api/v1/repositories?view=focus", 403}, {watch, "/api/v1/repositories?view=focus", 200}, {watch, "/api/v1/repositories", 403}, {watch, "/api/v1/repositories/1", 403}, {public, "/api/v1/repositories?page=0", 400}, {public, "/api/v1/repositories?page=1&page=2", 400}, {public, "/api/v1/repositories?size=1000", 400}, {public, "/api/v1/repositories?user_id=99", 400}, {public, "/api/v1/repositories?view=focus&focus=0", 400}, {public, "/api/v1/repositories/1?date=bad", 400}, {public, "/api/v1/repositories/9999", 404}} {
		w := f.request(t, test.key, "GET", test.target, "")
		if w.Code != test.status {
			t.Fatalf("%s: status=%d want=%d body=%s", test.target, w.Code, test.status, w.Body.String())
		}
		if test.key.Key.ID == public.Key.ID && test.status == 200 && strings.Contains(w.Body.String(), "is_focus") {
			t.Fatal("public scope exposed personal watch state")
		}
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "https://repotempo.test/api/v1/repositories?view=focus", nil)
	r.AddCookie(&http.Cookie{Name: "repotempo_session", Value: strings.Repeat("a", 64)})
	f.handler.ServeHTTP(w, r)
	if w.Code != 401 || w.Header().Get("Location") != "" {
		t.Fatal("API accepted cookie or redirected to browser login")
	}
}

func TestRemoteMCPHandshakeReadToolsAndNoWrites(t *testing.T) {
	f := newAgentAPIFixture(t)
	key := f.key(t, 42)
	call := func(body string) *httptest.ResponseRecorder {
		w := f.request(t, key, "POST", "/mcp", body)
		if w.Code != 200 {
			t.Fatalf("MCP HTTP status %d: %s", w.Code, w.Body.String())
		}
		return w
	}
	w := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"fixture","version":"1"}}}`)
	if !strings.Contains(w.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatal("MCP did not initialize")
	}
	w = call(`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
	if !strings.Contains(w.Body.String(), "list_watchlist") || strings.Contains(w.Body.String(), "delete_repository") {
		t.Fatal("wrong read-only tools")
	}
	w = call(`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_watchlist","arguments":{}}}`)
	var rpc struct {
		Result struct {
			Structured struct {
				Items []struct {
					ID int64 `json:"id"`
				} `json:"items"`
			} `json:"structuredContent"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &rpc); err != nil {
		t.Fatal(err)
	}
	if len(rpc.Result.Structured.Items) != 1 || rpc.Result.Structured.Items[0].ID != 1 {
		t.Fatalf("MCP watchlist isolation failed: %s", w.Body.String())
	}
	w = call(`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"delete_repository","arguments":{"id":1}}}`)
	if !strings.Contains(w.Body.String(), `"code":-32602`) {
		t.Fatal("write/unknown tool accepted")
	}
	w = call(`{"jsonrpc":"2.0","id":5,"method":"server/discover"}`)
	if !strings.Contains(w.Body.String(), `"code":-32601`) {
		t.Fatal("unsupported future handshake not rejected")
	}
	w = f.request(t, key, "POST", "/mcp", `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if w.Code != 202 {
		t.Fatal("notification requires 202")
	}
	w = call(`[{"jsonrpc":"2.0","id":6,"method":"tools/list"}]`)
	if !strings.Contains(w.Body.String(), `"code":-32600`) {
		t.Fatal("MCP batch unexpectedly accepted")
	}
}

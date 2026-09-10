package integrations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

type clientQuery struct {
	web.Queryer // Unrelated page methods must not be used by these API requests.
	seen        chan clientQueryObservation
}
type clientQueryObservation struct {
	query     web.RepositoryQuery
	principal domain.Principal
}

func (q clientQuery) ListRepositoryTrends(ctx context.Context, query web.RepositoryQuery) (web.RepositoryPage, error) {
	principal, _ := domain.PrincipalFromContext(ctx)
	q.seen <- clientQueryObservation{query: query, principal: principal}
	return web.RepositoryPage{Items: []web.RepositoryMetric{{ID: 42, FullName: "fixture/project", HTMLURL: "https://github.com/fixture/project", IsFocus: query.OnlyFocus}}, Total: 1}, nil
}

// Exercise the shipped Node client against real Go HTTP handlers, SQLite-backed
// credentials and replay storage. This catches cross-language signing drift.
func TestNodeClientUsesGoAccountAPIAndRevocation(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required for the agent client cross-language integration test")
	}
	ctx := context.Background()
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: filepath.Join(t.TempDir(), "client.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	hash := sha256.Sum256([]byte("Node client fixture browser session"))
	tokenHash := hex.EncodeToString(hash[:])
	if err := store.PutAuthSession(ctx, tokenHash, domain.AuthSession{GitHubUserID: 42, Login: "fixture-owner", CSRFToken: tokenHash, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	service, err := agentaccess.New(store, agentaccess.Options{})
	if err != nil {
		t.Fatal(err)
	}
	key, err := service.Create(ctx, 42, "Node fixture", nil, 30)
	if err != nil {
		t.Fatal(err)
	}
	queryer := clientQuery{seen: make(chan clientQueryObservation, 4)}
	nextQuery := func() clientQueryObservation {
		select {
		case observed := <-queryer.seen:
			return observed
		case <-time.After(5 * time.Second):
			t.Fatal("Node client did not reach the expected Go library query")
			return clientQueryObservation{}
		}
	}
	handler, err := web.New(queryer, web.Options{AgentAccess: service})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	script, err := filepath.Abs("repotempo-agent/scripts/cli.mjs")
	if err != nil {
		t.Fatal(err)
	}
	env := []string{}
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "REPOTEMPO_") {
			env = append(env, value)
		}
	}
	env = append(env, "REPOTEMPO_URL="+server.URL, "REPOTEMPO_AK="+key.Key.ID, "REPOTEMPO_SK="+key.Secret)
	call := func(args ...string) ([]byte, error) {
		requestContext, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		command := exec.CommandContext(requestContext, node, append([]string{script}, args...)...)
		command.Env = env
		return command.Output()
	}
	output, err := call("me")
	if err != nil {
		t.Fatalf("Node could not authenticate to Go account API: %v", err)
	}
	var account struct {
		User struct {
			ID    int64  `json:"github_user_id"`
			Login string `json:"login"`
		} `json:"user"`
	}
	if err := json.Unmarshal(output, &account); err != nil || account.User.ID != 42 || account.User.Login != "fixture-owner" {
		t.Fatal("Node client received the wrong account")
	}
	if _, err := call("repositories", `{"q":"中文 + & agent","page":2,"size":6,"sort":"growth_rate"}`); err != nil {
		t.Fatalf("encoded signed library query failed: %v", err)
	}
	observed := nextQuery()
	if observed.query.Search != "中文 + & agent" || observed.query.Offset != 6 || observed.query.Limit != 6 || observed.query.OnlyFocus || observed.principal.UserID != 42 || observed.principal.Admin {
		t.Fatal("signed query lost filters or account scope")
	}
	if _, err := call("watchlist"); err != nil {
		t.Fatalf("signed watchlist query failed: %v", err)
	}
	observed = nextQuery()
	if !observed.query.OnlyFocus || observed.principal.UserID != 42 || observed.principal.Admin {
		t.Fatal("watchlist client lost its non-admin account principal")
	}
	if err := service.Revoke(ctx, 42, key.Key.ID); err != nil {
		t.Fatal(err)
	}
	if output, err := call("me"); err == nil || len(output) != 0 {
		t.Fatal("Node client accepted a revoked key")
	} else if failed, ok := err.(*exec.ExitError); !ok || !strings.Contains(string(failed.Stderr), "authentication failed") || strings.Contains(string(failed.Stderr), key.Secret) {
		t.Fatal("Node revocation error was missing or exposed credentials")
	}
}

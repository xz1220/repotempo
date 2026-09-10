package web

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/service/agentaccess"
	loginauth "github.com/xz1220/repotempo/internal/service/auth"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

func newAgentAccountFixture(t *testing.T) *authHTTPFixture {
	t.Helper()
	f := newAuthHTTPFixture(t, true)
	store, err := sqlite.OpenWithConfig(context.Background(), sqlite.Config{Path: ":memory:", Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	f.service, err = loginauth.New(loginauth.Configuration{ClientID: "fixture-client-id", ClientSecret: "fixture-secret", PublicURL: authHTTPOrigin, AllowedUserIDs: []int64{42}, AllowPublicSignup: true}, store, loginauth.Options{Now: func() time.Time { return f.now }, Provider: f.provider})
	if err != nil {
		t.Fatal(err)
	}
	f.handler.auth = f.service
	f.handler.agentAccess, err = agentaccess.New(store, agentaccess.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func accountFormCSRF(t *testing.T, body string) string {
	t.Helper()
	match := regexp.MustCompile(`(?s)data-key-create.*?name="csrf_token" value="([^"]+)"`).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatal("missing account form token")
	}
	return match[1]
}
func accountForm(token string) url.Values {
	return url.Values{"csrf_token": {token}, "name": {"Research agent"}, "expires_days": {"90"}, "scope": {agentaccess.RepositoriesRead, agentaccess.WatchlistRead}}
}

func TestAgentAccountPublicSignupCreatesSecretOnceWithOneTimeCSRF(t *testing.T) {
	f := newAgentAccountFixture(t)
	f.provider.identity = loginauth.Identity{ID: 99, Login: "public-user"}
	cookie := f.login(t)
	page := f.send(f.request("GET", "/account/api", nil, cookie))
	if page.Code != 200 {
		t.Fatalf("ordinary user account status=%d %s", page.Code, page.Body.String())
	}
	csrf := accountFormCSRF(t, page.Body.String())
	foreign := f.request("POST", "/account/api/keys", accountForm(csrf), cookie)
	foreign.Header.Set("Origin", "https://evil.example")
	if w := f.send(foreign); w.Code != 403 {
		t.Fatal("foreign origin created key")
	}
	w := f.send(f.request("POST", "/account/api/keys", accountForm(csrf), cookie))
	if w.Code != 201 {
		t.Fatalf("key create status=%d body=%s", w.Code, w.Body.String())
	}
	secret := regexp.MustCompile(`id="api-new-sk"[^>]*value="([^"]+)"`).FindStringSubmatch(w.Body.String())
	if len(secret) != 2 || !strings.HasPrefix(secret[1], "rt_sk_") {
		t.Fatal("one-time secret missing")
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("secret response cache/referrer headers unsafe")
	}
	if again := f.send(f.request("POST", "/account/api/keys", accountForm(csrf), cookie)); again.Code != 403 {
		t.Fatal("duplicate form created another secret")
	}
	listed := f.send(f.request("GET", "/account/api", nil, cookie))
	if listed.Code != 200 || strings.Contains(listed.Body.String(), secret[1]) || strings.Contains(listed.Body.String(), "rt_sk_") {
		t.Fatal("retrieval showed issued secret")
	}
	keys, err := f.handler.agentAccess.List(context.Background(), 99)
	if err != nil || len(keys) != 1 {
		t.Fatal("creation not persisted once")
	}
	if strings.Contains(f.logs.String(), secret[1]) {
		t.Fatal("secret exposed in logs")
	}
	csrf = accountFormCSRF(t, listed.Body.String())
	form := accountForm(csrf)
	form.Del("scope")
	if denied := f.send(f.request("POST", "/account/api/keys?lang=zh-CN", form, cookie)); denied.Code != 400 || !strings.Contains(denied.Body.String(), "至少选择") {
		t.Fatal("no scopes silently gained permissions or untranslated error")
	}
}

func TestAgentAccountCannotListRevokeOrReuseOtherUsersForm(t *testing.T) {
	f := newAgentAccountFixture(t)
	f.provider.identity = loginauth.Identity{ID: 99, Login: "public-user"}
	owner := f.login(t)
	ownerPage := f.send(f.request("GET", "/account/api", nil, owner))
	ownerCSRF := accountFormCSRF(t, ownerPage.Body.String())
	key, err := f.handler.agentAccess.Create(context.Background(), 99, "Private agent", nil, 90)
	if err != nil {
		t.Fatal(err)
	}
	f.provider.identity = loginauth.Identity{ID: 42, Login: "site-admin"}
	other := f.login(t)
	otherPage := f.send(f.request("GET", "/account/api", nil, other))
	if otherPage.Code != 200 || strings.Contains(otherPage.Body.String(), key.Key.ID) {
		t.Fatal("administrator could list another user's keys")
	}
	path := "/account/api/keys/" + key.Key.ID + "/revoke"
	if w := f.send(f.request("POST", path, url.Values{"csrf_token": {ownerCSRF}}, other)); w.Code != 403 {
		t.Fatal("cross-session CSRF token accepted")
	}
	otherCSRF := accountFormCSRF(t, otherPage.Body.String())
	if w := f.send(f.request("POST", path, url.Values{"csrf_token": {otherCSRF}}, other)); w.Code != 404 {
		t.Fatal("administrator revoked another user's key")
	}
	if w := f.send(f.request("POST", path, url.Values{"csrf_token": {ownerCSRF}}, owner)); w.Code != http.StatusSeeOther {
		t.Fatalf("owner revocation failed %d %s", w.Code, w.Body.String())
	}
	keys, err := f.handler.agentAccess.List(context.Background(), 99)
	if err != nil || len(keys) != 1 || keys[0].RevokedAt == nil {
		t.Fatal("revocation not persisted")
	}
	if w := f.send(f.request("GET", "/account/api", nil)); w.Code != http.StatusSeeOther {
		t.Fatal("anonymous account listing accessible")
	}
	if w := f.send(f.request("POST", path, url.Values{"csrf_token": {ownerCSRF}})); w.Code != http.StatusUnauthorized {
		t.Fatal("anonymous revocation accessible")
	}
}

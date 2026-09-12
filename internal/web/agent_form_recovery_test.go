package web

import (
	"context"
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/service/agentaccess"
)

func TestAgentCreateErrorPreservesSettingsWithoutGrantingScopes(t *testing.T) {
	for _, locale := range []string{"en", "zh-CN"} {
		t.Run(locale, func(t *testing.T) {
			f := newAgentAccountFixture(t)
			cookie := f.login(t)
			page := f.send(f.request("GET", "/account/api?lang="+locale, nil, cookie))
			oldNonce := accountFormCSRF(t, page.Body.String())
			form := url.Values{"csrf_token": {oldNonce}, "name": {`Research <&> "agent"`}, "expires_days": {"30"}}
			failed := f.send(f.request("POST", "/account/api/keys?lang="+locale, form, cookie))
			body := failed.Body.String()
			if failed.Code != 400 {
				t.Fatalf("no scopes status=%d", failed.Code)
			}
			name := regexp.MustCompile(`id="api-key-name"[^>]*value="([^"]*)"`).FindStringSubmatch(body)
			if len(name) != 2 || html.UnescapeString(name[1]) != form.Get("name") {
				t.Error("validation discarded the entered name")
			}
			if !strings.Contains(body, `value="30" selected`) || strings.Contains(body, `name="scope" value="repositories:read" checked`) || strings.Contains(body, `name="scope" value="watchlist:read" checked`) {
				t.Error("validation expanded the selected expiry or permissions")
			}
			if !strings.Contains(body, `class="api-options" open`) || strings.Contains(body, "rt_sk_") {
				t.Error("error must expose settings, without a secret")
			}
			freshNonce := accountFormCSRF(t, body)
			if freshNonce == oldNonce {
				t.Fatal("error did not issue a fresh form nonce")
			}
			if replay := f.send(f.request("POST", "/account/api/keys", form, cookie)); replay.Code != 403 {
				t.Fatal("failed form nonce was replayable")
			}
			form.Set("csrf_token", freshNonce)
			form.Set("scope", agentaccess.RepositoriesRead)
			created := f.send(f.request("POST", "/account/api/keys", form, cookie))
			if created.Code != 201 {
				t.Fatalf("corrected submit=%d", created.Code)
			}
			keys, err := f.handler.agentAccess.List(context.Background(), 42)
			if err != nil || len(keys) != 1 || keys[0].Name != form.Get("name") || len(keys[0].Scopes) != 1 || keys[0].Scopes[0] != agentaccess.RepositoriesRead || !keys[0].ExpiresAt.Equal(f.now.Add(30*24*time.Hour)) {
				t.Fatalf("corrected key settings differ: keys=%+v err=%v", keys, err)
			}
		})
	}
}

func TestAgentCreateErrorsPreserveOnlySubmittedChoices(t *testing.T) {
	for _, tc := range []struct {
		name, expiry string
		scopes       []string
	}{
		{"", "365", []string{agentaccess.WatchlistRead}},
		{"Research", "invalid", []string{agentaccess.RepositoriesRead}},
		{"Research", "7", []string{agentaccess.WatchlistRead}},
	} {
		t.Run(tc.name+tc.expiry, func(t *testing.T) {
			f := newAgentAccountFixture(t)
			cookie := f.login(t)
			page := f.send(f.request("GET", "/account/api", nil, cookie))
			form := url.Values{"csrf_token": {accountFormCSRF(t, page.Body.String())}, "name": {tc.name}, "expires_days": {tc.expiry}, "scope": tc.scopes}
			failed := f.send(f.request("POST", "/account/api/keys", form, cookie))
			body := failed.Body.String()
			if failed.Code != 400 {
				t.Fatalf("status=%d", failed.Code)
			}
			for _, scope := range []string{agentaccess.RepositoriesRead, agentaccess.WatchlistRead} {
				selected := strings.Contains(body, `name="scope" value="`+scope+`" checked`)
				if selected != (scope == tc.scopes[0]) {
					t.Errorf("changed scope %s", scope)
				}
			}
			if strings.Contains(body, `value="90" selected`) {
				t.Error("invalid expiry expanded to the default")
			}
			if tc.expiry == "365" && !strings.Contains(body, `value="365" selected`) {
				t.Error("lost selected expiry")
			}
			if tc.expiry != "365" && !strings.Contains(body, `value="" selected disabled`) {
				t.Error("invalid expiry needs an explicit new choice")
			}
		})
	}
}

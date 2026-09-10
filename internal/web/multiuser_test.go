package web

import (
	"net/http"
	"net/url"
	"testing"

	loginauth "github.com/xz1220/repotempo/internal/service/auth"
)

func TestOrdinaryUserMayFollowButCannotReadAdministration(t *testing.T) {
	fixture := newAuthHTTPFixture(t, true)
	fixture.provider.identity = loginauth.Identity{ID: 101, Login: "ordinary-a"}
	a := fixture.login(t)
	updater := &fakeFocusUpdater{}
	fixture.handler.focusUpdater = updater
	for _, path := range []string{"/runs", "/watch/imports/" + authHTTPImportID, "/watch/imports/" + authHTTPImportID + "/status"} {
		before := fixture.queryer.calls + fixture.watcher.statuses
		response := fixture.send(fixture.request(http.MethodGet, path, nil, a))
		if response.Code != http.StatusForbidden || before != fixture.queryer.calls+fixture.watcher.statuses {
			t.Fatalf("ordinary admin access %s: %d", path, response.Code)
		}
	}
	form := fixture.send(fixture.request(http.MethodGet, "/repositories?view=focus&lang=en", nil, a))
	if form.Code != http.StatusOK {
		t.Fatalf("personal watchlist=%d", form.Code)
	}
	nonce := authResponseCookie(t, form, watchCookie)
	fixture.provider.identity = loginauth.Identity{ID: 202, Login: "ordinary-b"}
	b := fixture.login(t)
	cross := fixture.send(fixture.request(http.MethodPost, "/watch/focus", focusValues(nonce), b, nonce))
	if cross.Code != http.StatusConflict || updater.calls != 0 {
		t.Fatalf("cross-account CSRF accepted: %d", cross.Code)
	}
	accepted := fixture.send(fixture.request(http.MethodPost, "/watch/focus", focusValues(nonce), a, nonce))
	if accepted.Code != http.StatusSeeOther || updater.calls != 1 {
		t.Fatalf("own personal follow=%d", accepted.Code)
	}
	// Session rotation for the same user must invalidate old one-time forms.
	form = fixture.send(fixture.request(http.MethodGet, "/repositories?view=focus", nil, a))
	nonce = authResponseCookie(t, form, watchCookie)
	fixture.provider.identity = loginauth.Identity{ID: 101, Login: "ordinary-a"}
	newSession := fixture.login(t)
	replay := fixture.send(fixture.request(http.MethodPost, "/watch/focus", focusValues(nonce), newSession, nonce))
	if replay.Code != http.StatusConflict || updater.calls != 1 {
		t.Fatalf("cross-session CSRF accepted: %d", replay.Code)
	}
	session, err := fixture.service.Session(fixture.request(http.MethodGet, "/", nil).Context(), newSession.Value)
	if err != nil {
		t.Fatal(err)
	}
	logout := fixture.send(fixture.request(http.MethodPost, "/auth/logout", url.Values{"csrf_token": {session.CSRFToken}}, newSession))
	if logout.Code != http.StatusSeeOther {
		t.Fatalf("ordinary sign-out=%d", logout.Code)
	}
}

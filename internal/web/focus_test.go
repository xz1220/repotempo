package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type fakeFocusUpdater struct {
	calls int
	id    int64
	focus bool
	err   error
}

func (updater *fakeFocusUpdater) SetRepositoryFocus(ctx context.Context, id int64, focus bool) error {
	updater.calls++
	updater.id, updater.focus = id, focus
	if _, ok := ctx.Deadline(); !ok {
		panic("focus update missing deadline")
	}
	return updater.err
}

func focusTestHandler(t *testing.T, updater FocusUpdater, local bool, token string) *Handler {
	t.Helper()
	h, err := New(&fakeQueryer{}, Options{
		Watcher:          &fakeWatcher{},
		FocusUpdater:     updater,
		AllowLocalWrites: local,
		WriteToken:       token,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func focusPOST(base string, cookie *http.Cookie, values url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, base+"/watch/focus", strings.NewReader(values.Encode()))
	req.RemoteAddr = "127.0.0.1:5678"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	return req
}

func focusValues(cookie *http.Cookie) url.Values {
	return url.Values{
		"repository_id": {"101"},
		"focus":         {"1"},
		"return_to":     {"/repositories"},
		"csrf_token":    {cookie.Value},
	}
}

func TestWatchFocusPersistsExactChoiceRedirectsAndRejectsReplay(t *testing.T) {
	updater := &fakeFocusUpdater{}
	h := focusTestHandler(t, updater, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	returnTo := "/repositories?date=2026-09-10&focus=0&lang=zh-CN&new=0&period=7d&sort=stars&view=all#project-101"
	values := focusValues(cookie)
	values.Set("return_to", returnTo)

	first := httptest.NewRecorder()
	h.ServeHTTP(first, focusPOST(base, cookie, values))
	if first.Code != http.StatusSeeOther || first.Header().Get("Location") != returnTo {
		t.Fatalf("focus redirect = %d %q", first.Code, first.Header().Get("Location"))
	}
	if updater.calls != 1 || updater.id != 101 || !updater.focus {
		t.Fatalf("unexpected focus update: %+v", updater)
	}

	replay := httptest.NewRecorder()
	h.ServeHTTP(replay, focusPOST(base, cookie, values))
	if replay.Code != http.StatusConflict || updater.calls != 1 {
		t.Fatal("one-time focus form was replayed")
	}
}

func TestWatchFocusRejectsCrossOriginAndLegacyAuthorizationBeforeUpdate(t *testing.T) {
	const base = "http://127.0.0.1:8878"
	updater := &fakeFocusUpdater{}
	h := focusTestHandler(t, updater, true, "")
	cookie := watchFormCookie(t, h, base)
	crossOrigin := focusPOST(base, cookie, focusValues(cookie))
	crossOrigin.Header.Set("Origin", "https://private-attacker.example")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, crossOrigin)
	if response.Code != http.StatusForbidden || updater.calls != 0 {
		t.Fatal("cross-origin focus request reached updater")
	}

	const publicBase = "https://radar.example"
	const credential = "fixture-management-credential"
	public := focusTestHandler(t, updater, false, credential)
	publicCookie := watchFormCookie(t, public, publicBase)
	for _, supplied := range []string{"", "wrong-management-token"} {
		values := focusValues(publicCookie)
		if supplied != "" {
			values.Set("operator_token", supplied)
		}
		response = httptest.NewRecorder()
		public.ServeHTTP(response, focusPOST(publicBase, publicCookie, values))
		if response.Code != http.StatusForbidden || updater.calls != 0 {
			t.Fatalf("invalid legacy authority reached updater: %d", response.Code)
		}
		if supplied != "" && strings.Contains(response.Body.String(), supplied) || strings.Contains(response.Body.String(), credential) {
			t.Fatal("operator token leaked in denial")
		}
	}
	values := focusValues(publicCookie)
	values.Set("operator_token", credential)
	response = httptest.NewRecorder()
	public.ServeHTTP(response, focusPOST(publicBase, publicCookie, values))
	if response.Code != http.StatusSeeOther || updater.calls != 1 {
		t.Fatalf("valid legacy token did not authorize focus update: %d", response.Code)
	}
}

func TestWatchFocusOAuthOwnerGateRunsBeforeUpdater(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	updater := &fakeFocusUpdater{}
	fixture.handler.focusUpdater = updater
	publicPage := fixture.send(fixture.request(http.MethodGet, "/repositories?new=0&view=all", nil))
	if publicPage.Code != http.StatusOK || strings.Contains(publicPage.Body.String(), `action="/watch/focus"`) {
		t.Fatal("anonymous library exposed the owner focus control")
	}
	for _, cookie := range publicPage.Result().Cookies() {
		if cookie.Name == watchCookie {
			t.Fatal("anonymous library received a focus mutation nonce")
		}
	}
	csrf := strings.Repeat("a", 64)
	values := url.Values{
		"repository_id": {"101"}, "focus": {"1"}, "return_to": {"/repositories"},
		"csrf_token": {csrf}, "operator_token": {authHTTPOldToken},
	}
	request := fixture.request(http.MethodPost, "/watch/focus", values, &http.Cookie{Name: watchCookie, Value: csrf})
	response := fixture.send(request)
	if response.Code != http.StatusUnauthorized || updater.calls != 0 {
		t.Fatalf("anonymous focus mutation bypassed OAuth owner gate: %d", response.Code)
	}

	session := fixture.login(t)
	ownerPage := fixture.send(fixture.request(http.MethodGet, "/repositories?new=0&view=all", nil, session))
	if ownerPage.Code != http.StatusOK || !strings.Contains(ownerPage.Body.String(), `action="/watch/focus"`) {
		t.Fatal("authenticated owner did not receive the focus control")
	}
	nonce := authResponseCookie(t, ownerPage, watchCookie)
	ownerValues := url.Values{
		"repository_id": {"101"}, "focus": {"0"}, "return_to": {"/repositories"}, "csrf_token": {nonce.Value},
	}
	response = fixture.send(fixture.request(http.MethodPost, "/watch/focus", ownerValues, session, nonce))
	if response.Code != http.StatusSeeOther || updater.calls != 1 || updater.id != 101 || updater.focus {
		t.Fatalf("authenticated owner focus update failed: %d, %+v", response.Code, updater)
	}
}

func TestWatchFocusRejectsMalformedFormsBeforeUpdate(t *testing.T) {
	tests := []struct {
		name   string
		change func(*http.Request, url.Values)
		status int
	}{
		{"wrong media", func(r *http.Request, _ url.Values) { r.Header.Set("Content-Type", "application/json") }, http.StatusUnsupportedMediaType},
		{"malformed encoding", func(r *http.Request, _ url.Values) {
			r.URL.RawQuery = ""
			r.Body = io.NopCloser(strings.NewReader("repository_id=%zz"))
			r.ContentLength = int64(len("repository_id=%zz"))
		}, http.StatusBadRequest},
		{"missing id", func(_ *http.Request, v url.Values) { v.Del("repository_id") }, http.StatusBadRequest},
		{"duplicate id", func(_ *http.Request, v url.Values) { v.Add("repository_id", "101") }, http.StatusBadRequest},
		{"zero id", func(_ *http.Request, v url.Values) { v.Set("repository_id", "0") }, http.StatusBadRequest},
		{"leading zero id", func(_ *http.Request, v url.Values) { v.Set("repository_id", "0101") }, http.StatusBadRequest},
		{"signed id", func(_ *http.Request, v url.Values) { v.Set("repository_id", "+101") }, http.StatusBadRequest},
		{"overflow id", func(_ *http.Request, v url.Values) { v.Set("repository_id", "999999999999999999999999") }, http.StatusBadRequest},
		{"missing focus", func(_ *http.Request, v url.Values) { v.Del("focus") }, http.StatusBadRequest},
		{"duplicate focus", func(_ *http.Request, v url.Values) { v.Add("focus", "0") }, http.StatusBadRequest},
		{"boolean focus", func(_ *http.Request, v url.Values) { v.Set("focus", "true") }, http.StatusBadRequest},
		{"empty return", func(_ *http.Request, v url.Values) { v.Set("return_to", "") }, http.StatusBadRequest},
		{"external return", func(_ *http.Request, v url.Values) {
			v.Set("return_to", "https://private-attacker.example/repositories")
		}, http.StatusBadRequest},
		{"encoded return path", func(_ *http.Request, v url.Values) { v.Set("return_to", "/%72epositories") }, http.StatusBadRequest},
		{"unknown return query", func(_ *http.Request, v url.Values) {
			v.Set("return_to", "/repositories?next=https://private-attacker.example")
		}, http.StatusBadRequest},
		{"duplicate return", func(_ *http.Request, v url.Values) { v.Add("return_to", "/repositories") }, http.StatusBadRequest},
		{"missing csrf", func(_ *http.Request, v url.Values) { v.Del("csrf_token") }, http.StatusConflict},
		{"duplicate csrf", func(_ *http.Request, v url.Values) { v.Add("csrf_token", v.Get("csrf_token")) }, http.StatusConflict},
		{"duplicate operator token", func(_ *http.Request, v url.Values) { v["operator_token"] = []string{"one", "two"} }, http.StatusBadRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			updater := &fakeFocusUpdater{}
			h := focusTestHandler(t, updater, true, "")
			base := "http://127.0.0.1:8878"
			cookie := watchFormCookie(t, h, base)
			values := focusValues(cookie)
			request := focusPOST(base, cookie, values)
			test.change(request, values)
			if test.name != "wrong media" && test.name != "malformed encoding" {
				request.Body = io.NopCloser(strings.NewReader(values.Encode()))
				request.ContentLength = int64(len(values.Encode()))
			}
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			if response.Code != test.status || updater.calls != 0 {
				t.Fatalf("malformed request = %d, calls %d; want %d, 0", response.Code, updater.calls, test.status)
			}
			if strings.Contains(response.Body.String(), "private-attacker") {
				t.Fatal("untrusted form value leaked into error response")
			}
		})
	}

	t.Run("oversized", func(t *testing.T) {
		updater := &fakeFocusUpdater{}
		h := focusTestHandler(t, updater, true, "")
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8878/watch/focus", strings.NewReader("padding="+strings.Repeat("x", focusFormMaxBytes)))
		request.RemoteAddr = "127.0.0.1:5678"
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "http://127.0.0.1:8878")
		response := httptest.NewRecorder()
		h.ServeHTTP(response, request)
		if response.Code != http.StatusRequestEntityTooLarge || updater.calls != 0 {
			t.Fatal("oversized focus form reached updater")
		}
	})
}

func TestWatchFocusRejectsUnsafeReturnWithoutConsumingNonce(t *testing.T) {
	updater := &fakeFocusUpdater{}
	h := focusTestHandler(t, updater, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	values := focusValues(cookie)
	values.Set("return_to", "//private-attacker.example/repositories")
	response := httptest.NewRecorder()
	h.ServeHTTP(response, focusPOST(base, cookie, values))
	if response.Code != http.StatusBadRequest || updater.calls != 0 || response.Header().Get("Location") != "" {
		t.Fatal("unsafe return target was accepted")
	}
	values.Set("return_to", "/repositories")
	response = httptest.NewRecorder()
	h.ServeHTTP(response, focusPOST(base, cookie, values))
	if response.Code != http.StatusSeeOther || updater.calls != 1 {
		t.Fatal("redirect validation consumed an otherwise valid nonce")
	}
}

func TestWatchFocusRejectsDuplicateNonceCookieWithoutConsumingNonce(t *testing.T) {
	updater := &fakeFocusUpdater{}
	h := focusTestHandler(t, updater, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	request := focusPOST(base, cookie, focusValues(cookie))
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || updater.calls != 0 {
		t.Fatal("duplicate watch nonce cookies were accepted")
	}
	response = httptest.NewRecorder()
	h.ServeHTTP(response, focusPOST(base, cookie, focusValues(cookie)))
	if response.Code != http.StatusSeeOther || updater.calls != 1 {
		t.Fatal("duplicate cookie rejection consumed the valid nonce")
	}
}

func TestWatchFocusErrorsAreStableAndDoNotLeakUpdaterDetails(t *testing.T) {
	secret := "sqlite:///private/operator/radar.db?token=do-not-leak"
	updater := &fakeFocusUpdater{err: errors.New(secret)}
	h := focusTestHandler(t, updater, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, focusPOST(base, cookie, focusValues(cookie)))
	if response.Code != http.StatusServiceUnavailable || updater.calls != 1 {
		t.Fatalf("updater failure status %d", response.Code)
	}
	if strings.Contains(response.Body.String(), secret) || response.Header().Get("Location") != "" || !strings.Contains(response.Body.String(), focusText(localeEnglish, "unavailable")) {
		t.Fatal("focus update leaked private failure details")
	}
}

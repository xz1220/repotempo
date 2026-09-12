package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWatchLegacyPagesKeepIndependentNoncesWithStableBrowserBinding(t *testing.T) {
	h := focusTestHandler(t, &fakeFocusUpdater{}, true, "")
	base := "http://127.0.0.1:8878"
	binding := watchFormCookie(t, h, base)
	if binding.Path != "/" || !binding.HttpOnly || binding.SameSite != http.SameSiteStrictMode {
		t.Fatalf("browser binding attributes: %+v", binding)
	}
	r := httptest.NewRequest(http.MethodGet, base+"/repositories?view=all&new=0", nil)
	r.RemoteAddr = "127.0.0.1:50101"
	r.AddCookie(binding)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	secondBinding := authResponseCookie(t, w, watchCookie)
	if secondBinding.Value != binding.Value {
		t.Fatal("library GET replaced browser binding")
	}
	// issueWatchNonce is also used for an empty library without rendered rows.
	secondToken, err := h.issueWatchNonce(httptest.NewRecorder(), r)
	if err != nil {
		t.Fatal(err)
	}
	if secondToken == binding.Value {
		t.Fatal("second page reused the first form token")
	}
	for _, token := range []string{binding.Value, secondToken} {
		values := focusValues(binding)
		values.Set("csrf_token", token)
		foreign := *binding
		foreign.Value = strings.Repeat("b", 64)
		w = httptest.NewRecorder()
		h.ServeHTTP(w, focusPOST(base, &foreign, values))
		if w.Code != http.StatusConflict {
			t.Fatalf("foreign browser binding=%d", w.Code)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, focusPOST(base, secondBinding, values))
		if w.Code != http.StatusSeeOther {
			t.Fatalf("independent page=%d", w.Code)
		}
		w = httptest.NewRecorder()
		h.ServeHTTP(w, focusPOST(base, secondBinding, values))
		if w.Code != http.StatusConflict {
			t.Fatalf("replay=%d", w.Code)
		}
	}
}

func TestWatchAuthenticatedNonceExpiresAndRejectsCrossOrigin(t *testing.T) {
	f := newAuthHTTPFixture(t, true)
	session := f.login(t)
	updater := &fakeFocusUpdater{}
	f.handler.focusUpdater = updater
	page := f.send(f.request(http.MethodGet, "/repositories?view=all", nil, session))
	cookie := authResponseCookie(t, page, watchCookie)
	values := focusValues(cookie)
	request := f.request(http.MethodPost, "/watch/focus", values, session, cookie)
	request.Header.Set("Origin", "https://attacker.example")
	if w := f.send(request); w.Code != http.StatusForbidden || updater.calls != 0 {
		t.Fatalf("cross-origin=%d", w.Code)
	}
	f.now = f.now.Add(31 * time.Minute)
	if w := f.send(f.request(http.MethodPost, "/watch/focus", values, session, cookie)); w.Code != http.StatusConflict || updater.calls != 0 {
		t.Fatalf("expired nonce=%d", w.Code)
	}
}

func TestWatchNonceConcurrentConsumptionHasOneWinner(t *testing.T) {
	h := focusTestHandler(t, &fakeFocusUpdater{}, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	results := make(chan bool, 2)
	for range 2 {
		go func() { results <- h.consumeWatchNonce(focusPOST(base, cookie, focusValues(cookie)), cookie.Value) }()
	}
	accepted := 0
	for range 2 {
		if <-results {
			accepted++
		}
	}
	if accepted != 1 {
		t.Fatalf("same form accepted %d times", accepted)
	}
}

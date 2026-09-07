package web

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeWatcher struct {
	calls   int
	input   WatchRequest
	err     error
	block   <-chan struct{}
	started chan<- struct{}
}

func (f *fakeWatcher) AddWatch(ctx context.Context, input WatchRequest) (WatchResult, error) {
	f.calls++
	f.input = input
	if _, ok := ctx.Deadline(); !ok {
		panic("watch missing deadline")
	}
	if f.started != nil {
		f.started <- struct{}{}
	}
	if f.block != nil {
		<-f.block
	}
	return WatchResult{ID: 99, FullName: "owner/repo", Created: true}, f.err
}

func (f *fakeWatcher) WatchTopics(context.Context) ([]TopicRef, error) {
	return []TopicRef{{Slug: "coding-agents", Name: "编程助手"}}, nil
}

func watchTestHandler(t *testing.T, watcher *fakeWatcher, local bool, token string) *Handler {
	t.Helper()
	h, err := New(&fakeQueryer{}, Options{Watcher: watcher, AllowLocalWrites: local, WriteToken: token})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func watchFormCookie(t *testing.T, handler http.Handler, base string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, base+"/watch/new?lang=zh-CN", nil)
	req.RemoteAddr = "127.0.0.1:5678"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("form status: %d", rec.Code)
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == watchCookie {
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatal("unsafe CSRF cookie")
			}
			return cookie
		}
	}
	t.Fatalf("no CSRF cookie: %s", rec.Body.String())
	return nil
}

func watchPOST(base string, cookie *http.Cookie, token string) *http.Request {
	values := url.Values{"repository": {"owner/repo"}, "topic": {"coding-agents"}, "note": {"关注中文编程"}, "csrf_token": {cookie.Value}, "operator_token": {token}}
	req := httptest.NewRequest(http.MethodPost, base+"/watch?lang=zh-CN", strings.NewReader(values.Encode()))
	req.RemoteAddr = "127.0.0.1:5678"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	req.AddCookie(cookie)
	return req
}

func TestWatchLocalAddAndReplayProtection(t *testing.T) {
	watcher := &fakeWatcher{}
	h := watchTestHandler(t, watcher, true, "")
	cookie := watchFormCookie(t, h, "http://127.0.0.1:8878")
	for index, want := range []int{http.StatusSeeOther, http.StatusConflict} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, watchPOST("http://127.0.0.1:8878", cookie, ""))
		if rec.Code != want {
			t.Fatalf("submit %d status %d, want %d", index, rec.Code, want)
		}
		if index == 0 && rec.Header().Get("Location") != "/repositories/99?lang=zh-CN" {
			t.Fatal("missing detail redirect")
		}
	}
	if watcher.calls != 1 || watcher.input.TopicSlug != "coding-agents" || watcher.input.Note != "关注中文编程" {
		t.Fatalf("unexpected writes: %#v", watcher)
	}
}

func TestWatchRejectsCrossOriginAndProxyLocalBypass(t *testing.T) {
	for _, kind := range []string{"cross-origin", "missing-origin", "cross-site", "remote-peer", "rebound-host", "forwarded"} {
		t.Run(kind, func(t *testing.T) {
			watcher := &fakeWatcher{}
			h := watchTestHandler(t, watcher, true, "")
			cookie := watchFormCookie(t, h, "http://127.0.0.1:8878")
			req := watchPOST("http://127.0.0.1:8878", cookie, "")
			switch kind {
			case "cross-origin":
				req.Header.Set("Origin", "https://evil.example")
			case "missing-origin":
				req.Header.Del("Origin")
			case "cross-site":
				req.Header.Set("Sec-Fetch-Site", "cross-site")
			case "remote-peer":
				req.RemoteAddr = "192.0.2.5:5678"
			case "rebound-host":
				req.Host = "evil.example"
				req.Header.Set("Origin", "http://evil.example")
			case "forwarded":
				req.Header.Set("X-Forwarded-For", "192.0.2.5")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden || watcher.calls != 0 {
				t.Fatalf("unsafe request accepted: %d", rec.Code)
			}
		})
	}
}

func TestWatchPublicRequiresConfiguredTokenAndHTTPS(t *testing.T) {
	const token = "fixture-management-token-not-a-secret"
	watcher := &fakeWatcher{}
	h := watchTestHandler(t, watcher, false, token)
	cookie := watchFormCookie(t, h, "https://radar.example")
	for _, supplied := range []string{"", "incorrect", token} {
		req := watchPOST("https://radar.example", cookie, supplied)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		want := http.StatusForbidden
		if supplied == token {
			want = http.StatusSeeOther
		}
		if rec.Code != want {
			t.Fatalf("public add status %d, want %d", rec.Code, want)
		}
		if strings.Contains(rec.Body.String(), token) {
			t.Fatal("management token leaked")
		}
	}
	if watcher.calls != 1 {
		t.Fatalf("writes %d, want one", watcher.calls)
	}
	for _, secure := range []bool{false, true} {
		readOnly := watchTestHandler(t, &fakeWatcher{}, false, "")
		req := httptest.NewRequest(http.MethodGet, "http://radar.example/watch/new", nil)
		if secure {
			req.TLS = &tls.ConnectionState{}
		}
		rec := httptest.NewRecorder()
		readOnly.ServeHTTP(rec, req)
		if strings.Contains(rec.Body.String(), `name="operator_token"`) || strings.Contains(rec.Body.String(), `name="repository"`) {
			t.Fatal("public site without token exposed write form")
		}
	}
	request := watchPOST("http://radar.example", cookie, token)
	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatal("cleartext management token was accepted")
	}
}

func TestWatchErrorsPreserveInputAndHideUpstreamMessages(t *testing.T) {
	watcher := &fakeWatcher{err: errors.New("Authorization: fixture-upstream-secret /private/database.db")}
	h := watchTestHandler(t, watcher, true, "")
	cookie := watchFormCookie(t, h, "http://localhost:8878")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, watchPOST("http://localhost:8878", cookie, ""))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "owner/repo") || !strings.Contains(body, "关注中文编程") {
		t.Fatal("lost input")
	}
	if strings.Contains(body, "fixture-upstream-secret") || strings.Contains(body, "/private/database.db") {
		t.Fatal("upstream error leaked")
	}
}

func TestWatchRejectsOversizedRequestAndExpiredForm(t *testing.T) {
	watcher := &fakeWatcher{}
	h := watchTestHandler(t, watcher, true, "")
	base := "http://127.0.0.1:8878"
	cookie := watchFormCookie(t, h, base)
	req := httptest.NewRequest(http.MethodPost, base+"/watch", strings.NewReader("note="+strings.Repeat("x", 25000)))
	req.RemoteAddr = "127.0.0.1:5678"
	req.Header.Set("Origin", base)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge || watcher.calls != 0 {
		t.Fatal("oversized body accepted")
	}
	h.watchMu.Lock()
	h.watchNonces[cookie.Value] = time.Now().Add(-time.Hour)
	h.watchMu.Unlock()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, watchPOST(base, cookie, ""))
	if rec.Code != http.StatusConflict || watcher.calls != 0 {
		t.Fatal("expired form accepted")
	}
}

func TestWatchBlocksConcurrentSubmissions(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	watcher := &fakeWatcher{started: started, block: release}
	h := watchTestHandler(t, watcher, true, "")
	base := "http://127.0.0.1:8878"
	first, second := watchFormCookie(t, h, base), watchFormCookie(t, h, base)
	done := make(chan struct{})
	go func() { defer close(done); h.ServeHTTP(httptest.NewRecorder(), watchPOST(base, first, "")) }()
	<-started
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, watchPOST(base, second, ""))
	close(release)
	<-done
	if rec.Code != http.StatusTooManyRequests || watcher.calls != 1 {
		t.Fatal("concurrent submit not blocked")
	}
}

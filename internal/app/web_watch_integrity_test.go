package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	loginauth "github.com/xz1220/repotempo/internal/service/auth"
	"github.com/xz1220/repotempo/internal/web"
)

type personalWatchFixture struct {
	runtime *Runtime
	handler *web.Handler
	now     time.Time
	base    string
}

func newPersonalWatchFixture(t *testing.T, base string, emptyCatalogue ...bool) *personalWatchFixture {
	t.Helper()
	f := &personalWatchFixture{now: time.Date(2026, 9, 12, 4, 0, 0, 0, time.UTC), base: base}
	var err error
	f.runtime, err = OpenRuntimeWithOptions(context.Background(), Settings{DatabasePath: t.TempDir() + "/personal-watch.db"}, RuntimeOptions{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.runtime.Close() })
	for id := int64(1); id <= 2; id++ {
		if len(emptyCatalogue) > 0 && emptyCatalogue[0] {
			break
		}
		_, _, err = f.runtime.store.UpsertRepository(context.Background(), domain.RepositoryObservation{GitHubRepoID: id, FullName: fmt.Sprintf("owner/project-%d", id), Source: domain.DiscoverySourceManual, DiscoveredAt: f.now.AddDate(0, 0, -10)})
		if err != nil {
			t.Fatal(err)
		}
	}
	auth, err := loginauth.New(loginauth.Configuration{ClientID: "isolated-test", ClientSecret: "isolated-test", PublicURL: base, AllowedUserIDs: []int64{42}, AllowPublicSignup: true}, f.runtime.store, loginauth.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := agentaccess.New(f.runtime.store, agentaccess.Options{Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.handler, err = web.New(WebAdapter{Store: f.runtime.store}, web.Options{AgentAccess: keys, Watcher: f.runtime, FocusUpdater: f.runtime, Auth: auth, Now: func() time.Time { return f.now }, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *personalWatchFixture) session(t *testing.T, userID int64, serial int) *http.Cookie {
	t.Helper()
	raw := fmt.Sprintf("%064x", userID*1000+int64(serial))
	hash := sha256.Sum256([]byte(raw))
	err := f.runtime.store.PutAuthSession(context.Background(), hex.EncodeToString(hash[:]), domain.AuthSession{GitHubUserID: userID, Login: fmt.Sprintf("preview-user-%d", userID), CSRFToken: fmt.Sprintf("%064x", userID*10000+int64(serial)), CreatedAt: f.now, ExpiresAt: f.now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	return &http.Cookie{Name: "repotempo_session", Value: raw, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode}
}

func (f *personalWatchFixture) request(method, path string, form url.Values, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, f.base+path, strings.NewReader(form.Encode()))
	r.RemoteAddr = "127.0.0.1:51345"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", f.base)
	}
	for _, cookie := range cookies {
		if cookie != nil {
			r.AddCookie(cookie)
		}
	}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	return w
}

func personalWatchNonce(t *testing.T, w *httptest.ResponseRecorder) (string, *http.Cookie) {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("form=%d: %s", w.Code, w.Body.String())
	}
	matches := regexp.MustCompile(`name="csrf_token" value="([a-f0-9]{64})"`).FindAllStringSubmatch(w.Body.String(), -1)
	if len(matches) == 0 {
		t.Fatal("missing one-time form token")
	}
	match := matches[len(matches)-1]
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "repotempo_watch_binding" {
			return match[1], cookie
		}
	}
	return match[1], nil
}

func TestPersonalWatchDuplicateAddPreservesSQLiteState(t *testing.T) {
	f := newPersonalWatchFixture(t, "http://127.0.0.1:18879")
	session := f.session(t, 101, 1)
	for _, id := range []int64{101, 202} {
		f.session(t, id, 3)
		if err := f.runtime.store.SetUserRepositoryState(context.Background(), id, 1, true, fmt.Sprintf("private note %d", id)); err != nil {
			t.Fatal(err)
		}
	}
	token, cookie := personalWatchNonce(t, f.request(http.MethodGet, "/watch/new", nil, session))
	result := f.request(http.MethodPost, "/watch", url.Values{"repository": {"https://github.com/owner/project-1"}, "note": {""}, "csrf_token": {token}}, session, cookie)
	if result.Code != http.StatusSeeOther {
		t.Fatalf("duplicate add=%d: %s", result.Code, result.Body.String())
	}
	for _, id := range []int64{101, 202} {
		states, err := f.runtime.store.UserRepositoryStates(context.Background(), id, []int64{1})
		if err != nil || !states[1].IsFocus || states[1].Note != fmt.Sprintf("private note %d", id) {
			t.Fatalf("duplicate add changed user %d: %+v %v", id, states, err)
		}
	}
	if f.runtime.loaded {
		t.Fatal("personal duplicate add reached upstream dependencies")
	}
}

func TestPersonalWatchFormsSurviveOtherPagesAndKeepOneTimeSessionBinding(t *testing.T) {
	for _, secondPath := range []string{"/watch/new", "/repositories?view=all&new=0"} {
		t.Run(secondPath, func(t *testing.T) {
			f := newPersonalWatchFixture(t, "http://127.0.0.1:18879")
			session := f.session(t, 101, 1)
			oldToken, _ := personalWatchNonce(t, f.request(http.MethodGet, "/repositories?view=all&new=0", nil, session))
			newToken, latestCookie := personalWatchNonce(t, f.request(http.MethodGet, secondPath, nil, session))
			form := url.Values{"repository_id": {"1"}, "focus": {"1"}, "return_to": {"/repositories?view=all&new=0"}, "csrf_token": {oldToken}}
			if w := f.request(http.MethodPost, "/watch/focus", form, f.session(t, 202, 1), latestCookie); w.Code != http.StatusConflict {
				t.Fatalf("cross-user=%d", w.Code)
			}
			if w := f.request(http.MethodPost, "/watch/focus", form, f.session(t, 101, 2), latestCookie); w.Code != http.StatusConflict {
				t.Fatalf("cross-session=%d", w.Code)
			}
			if w := f.request(http.MethodPost, "/watch/focus", form, session, latestCookie); w.Code != http.StatusSeeOther {
				t.Fatalf("older page focus=%d: %s", w.Code, w.Body.String())
			}
			if w := f.request(http.MethodPost, "/watch/focus", form, session, latestCookie); w.Code != http.StatusConflict {
				t.Fatalf("replay=%d", w.Code)
			}
			form.Set("csrf_token", newToken)
			form.Set("focus", "0")
			if w := f.request(http.MethodPost, "/watch/focus", form, session, latestCookie); w.Code != http.StatusSeeOther {
				t.Fatalf("second page was consumed with first: %d", w.Code)
			}
			states, err := f.runtime.store.UserRepositoryStates(context.Background(), 101, []int64{1})
			if err != nil || states[1].IsFocus {
				t.Fatalf("focus not durable: %+v %v", states, err)
			}
		})
	}
}

func TestPersonalWatchExplicitEditCanClearButCannotRetarget(t *testing.T) {
	for _, userID := range []int64{101, 42} {
		t.Run(fmt.Sprintf("user-%d", userID), func(t *testing.T) {
			f := newPersonalWatchFixture(t, "http://127.0.0.1:18879")
			session := f.session(t, userID, 1)
			for _, id := range []int64{1, 2} {
				if err := f.runtime.store.SetUserRepositoryState(context.Background(), userID, id, true, "saved note"); err != nil {
					t.Fatal(err)
				}
			}

			// Follow the detail page entry a user can actually click.
			if _, err := f.runtime.store.PutRepositoryAnalysis(context.Background(), domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "Shared research summary", Source: "human"}); err != nil {
				t.Fatal(err)
			}
			detail := f.request(http.MethodGet, "/repositories/1?lang=en", nil, session)
			link := regexp.MustCompile(`href="([^"]+)" data-edit-personal-note`).FindStringSubmatch(detail.Body.String())
			if detail.Code != http.StatusOK || len(link) != 2 || !strings.Contains(detail.Body.String(), "saved note") {
				t.Fatal("detail omitted personal note or its edit entry")
			}
			editURL := html.UnescapeString(link[1])
			if !strings.Contains(editURL, "lang=en") {
				t.Fatal("edit entry lost language")
			}
			anonymous := f.request(http.MethodGet, "/repositories/1?lang=en", nil)
			if strings.Contains(anonymous.Body.String(), "data-edit-personal-note") || strings.Contains(anonymous.Body.String(), "saved note") {
				t.Fatal("anonymous detail exposed personal note actions")
			}
			page := f.request(http.MethodGet, editURL, nil, session)
			token, cookie := personalWatchNonce(t, page)
			if !strings.Contains(page.Body.String(), `name="edit_repository" value="owner/project-1"`) || !strings.Contains(page.Body.String(), "Leaving this blank clears your note") {
				t.Fatal("prefilled form did not explain explicit edits")
			}
			form := url.Values{"repository": {"owner/project-2"}, "edit_repository": {"owner/project-1"}, "focus": {"0"}, "note": {""}, "csrf_token": {token}}
			rejected := f.request(http.MethodPost, "/watch", form, session, cookie)
			if rejected.Code != http.StatusBadRequest {
				t.Fatalf("retargeted edit=%d", rejected.Code)
			}
			// A recoverable error keeps the exact editing target and choices.
			if !strings.Contains(rejected.Body.String(), `name="edit_repository" value="owner/project-1"`) {
				t.Fatal("error lost edit intent")
			}
			page = f.request(http.MethodGet, editURL, nil, session)
			token, cookie = personalWatchNonce(t, page)
			form.Set("csrf_token", token)
			form.Set("repository", "owner/project-1")
			if w := f.request(http.MethodPost, "/watch", form, session, cookie); w.Code != http.StatusSeeOther {
				t.Fatalf("explicit clear=%d", w.Code)
			}
			states, err := f.runtime.store.UserRepositoryStates(context.Background(), userID, []int64{1, 2})
			if err != nil || states[1].IsFocus || states[1].Note != "" || !states[2].IsFocus || states[2].Note != "saved note" {
				t.Fatalf("explicit clear state=%+v %v", states, err)
			}

		})
	}
}

func TestPersonalWatchAddFillsEmptyStateAndKeepsExistingNote(t *testing.T) {
	f := newPersonalWatchFixture(t, "http://127.0.0.1:18879")
	f.session(t, 101, 1)
	ctx := domain.WithPrincipal(context.Background(), domain.Principal{UserID: 101})
	for _, request := range []web.WatchRequest{{Repository: "owner/project-1", Note: "first note"}, {Repository: "owner/project-1", Focus: true, Note: "replacement is not an edit"}, {Repository: "owner/project-1"}} {
		if _, err := f.runtime.AddWatch(ctx, request); err != nil {
			t.Fatal(err)
		}
	}
	state, err := f.runtime.WatchState(ctx, "owner/project-1")
	if err != nil || !state.Focus || state.Note != "first note" {
		t.Fatalf("add merge state=%+v %v", state, err)
	}
}

func TestAdministratorImportRetryKeepsImportMode(t *testing.T) {
	f := newPersonalWatchFixture(t, "http://127.0.0.1:18879")
	session := f.session(t, 42, 1)
	for _, item := range []struct {
		path    string
		editing bool
	}{{"/watch/new?repository=owner/project-1", true}, {"/watch/new?repository=owner/project-1&import=1", false}} {
		page := f.request(http.MethodGet, item.path, nil, session)
		if page.Code != http.StatusOK {
			t.Fatalf("form=%d", page.Code)
		}
		hasEdit := strings.Contains(page.Body.String(), `name="edit_repository"`)
		hasTopic := strings.Contains(page.Body.String(), `id="watch-topic"`)
		if hasEdit != item.editing || hasTopic == item.editing {
			t.Fatalf("admin mode edit=%t topic=%t wantEdit=%t", hasEdit, hasTopic, item.editing)
		}
	}
}

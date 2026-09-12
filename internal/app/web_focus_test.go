package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebFocusTogglePersistsInSQLite(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
	runtime, err := OpenRuntimeWithOptions(ctx, Settings{DatabasePath: t.TempDir() + "/focus.db"}, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	if _, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 101,
		FullName:     "owner/focus-project",
		Source:       domain.DiscoverySourceGitHubTrending,
		DiscoveredAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	handler, err := web.New(WebAdapter{Store: runtime.store}, web.Options{
		Watcher:          runtime,
		FocusUpdater:     runtime,
		AllowLocalWrites: true,
		Now:              func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:8878"

	for _, focus := range []string{"1", "0"} {
		formRequest := httptest.NewRequest(http.MethodGet, base+"/repositories?date=2026-09-10&new=0&view=all&lang=en", nil)
		formRequest.RemoteAddr = "127.0.0.1:50101"
		formResponse := httptest.NewRecorder()
		handler.ServeHTTP(formResponse, formRequest)
		if formResponse.Code != http.StatusOK || !strings.Contains(formResponse.Body.String(), `action="/watch/focus"`) || !strings.Contains(formResponse.Body.String(), `name="repository_id" value="101"`) || !strings.Contains(formResponse.Body.String(), `name="focus" value="`+focus+`"`) {
			t.Fatalf("library did not render the real focus form for value %s: %d", focus, formResponse.Code)
		}
		var nonce *http.Cookie
		for _, cookie := range formResponse.Result().Cookies() {
			if cookie.Name == "repotempo_watch_binding" {
				nonce = cookie
			}
		}
		if nonce == nil {
			t.Fatal("missing watch CSRF nonce")
		}
		if !strings.Contains(formResponse.Body.String(), `name="csrf_token" value="`+nonce.Value+`"`) {
			t.Fatal("library focus form did not receive its one-time nonce")
		}
		values := url.Values{
			"repository_id": {"101"},
			"focus":         {focus},
			"return_to":     {"/repositories?focus=0&new=0&view=all#project-101"},
			"csrf_token":    {nonce.Value},
		}
		request := httptest.NewRequest(http.MethodPost, base+"/watch/focus", strings.NewReader(values.Encode()))
		request.RemoteAddr = "127.0.0.1:50101"
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", base)
		request.AddCookie(nonce)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusSeeOther || response.Header().Get("Location") != values.Get("return_to") {
			t.Fatalf("focus %s response = %d %q", focus, response.Code, response.Header().Get("Location"))
		}
		repository, err := runtime.store.GetRepository(ctx, 101)
		if err != nil || repository.IsFocus != (focus == "1") {
			t.Fatalf("focus %s was not stored: %+v, %v", focus, repository, err)
		}
	}
}

func TestRuntimeFocusErrorsStayInsideWebContract(t *testing.T) {
	runtime, err := OpenRuntime(context.Background(), Settings{DatabasePath: t.TempDir() + "/focus-errors.db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRepositoryFocus(context.Background(), 0, true); !errors.Is(err, web.ErrFocusInvalid) {
		t.Fatalf("invalid ID error = %v", err)
	}
	if err := runtime.SetRepositoryFocus(context.Background(), 999, true); !errors.Is(err, web.ErrFocusNotFound) {
		t.Fatalf("missing repository error = %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetRepositoryFocus(context.Background(), 999, true); !errors.Is(err, web.ErrFocusUnavailable) {
		t.Fatalf("closed store error escaped Web contract: %v", err)
	}
}

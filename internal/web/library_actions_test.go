package web

import (
	"html"
	"net/http"
	"strings"
	"testing"
)

func TestLibraryRetainsWatchAndNavigationWithoutReadMarks(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	fixture.handler.focusUpdater = &fakeFocusUpdater{}
	cookie := fixture.login(t)
	for _, locale := range []string{localeEnglish, localeChinese} {
		t.Run(locale, func(t *testing.T) {
			response := fixture.send(fixture.request(http.MethodGet, "/repositories?view=all&new=0&lang="+locale, nil, cookie))
			body := html.UnescapeString(response.Body.String())
			if response.Code != http.StatusOK {
				t.Fatalf("library status = %d", response.Code)
			}
			for _, removed := range []string{"reading-state.js", "data-reading-", "project-read-toggle", "project-reading-note", "reading-local-note", "标记已读", "标记未读", "Mark as read", "mark as unread"} {
				if strings.Contains(body, removed) {
					t.Errorf("library still exposes removed read marks: %s", removed)
				}
			}
			for _, preserved := range []string{`data-repository-id="101"`, `data-repository-detail`, `return_to=`, `data-focus-form`, `action="/watch/focus"`, `/static/reading-position.js`, `class="project-brief"`} {
				if !strings.Contains(body, preserved) {
					t.Errorf("library lost existing functionality: %s", preserved)
				}
			}
		})
	}
	if response := fixture.send(fixture.request(http.MethodGet, "/static/reading-state.js", nil)); response.Code != http.StatusNotFound {
		t.Fatalf("removed read-state asset status = %d; want 404", response.Code)
	}
}

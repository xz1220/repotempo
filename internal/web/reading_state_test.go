package web

import (
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestReadingStateAssetIsRegisteredWithJavaScriptMIME(t *testing.T) {
	handler := newTestHandler(t, populatedFake())
	response := request(t, handler, "/static/reading-state.js")
	if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/javascript; charset=utf-8" {
		t.Fatalf("reading script response = %d, %q", response.Code, response.Header().Get("Content-Type"))
	}
	if !strings.Contains(response.Body.String(), "repotempo:reading:v1:") {
		t.Fatal("reading script did not serve the browser-local implementation")
	}
	body := request(t, handler, "/repositories").Body.String()
	if strings.Count(body, `<script defer src="/static/reading-state.js"></script>`) != 1 {
		t.Fatal("the reading script must load once as a deferred same-origin asset")
	}
}

func TestReadingControlsHaveStableIDsLocalizedActionsAndOneSiteNotice(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			first := queryer.repositories.Items[0]
			second := first
			second.ID, second.FullName = 102, "acme/other-radar"
			queryer.repositories.Items, queryer.repositories.Total = []RepositoryMetric{first, second}, 2
			body := html.UnescapeString(request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories?date=2026-08-30&lang="+locale).Body.String())
			l := newLocalizer(locale)
			if strings.Count(body, l.Text("feed.reading_local_note")) != 1 || strings.Count(body, `id="reading-local-note"`) != 1 {
				t.Fatal("the browser/site-only notice must appear once, not on every card")
			}
			if locale == localeChinese && !strings.Contains(body, "仅保存在当前浏览器和站点，不跨设备同步") {
				t.Fatal("the notice must distinguish browser and site storage boundaries")
			}
			buttons := regexp.MustCompile(`<button\b[^>]*data-reading-toggle[^>]*>`).FindAllString(body, -1)
			if len(buttons) != 2 {
				t.Fatalf("got %d reading buttons for two projects", len(buttons))
			}
			for index, item := range []RepositoryMetric{first, second} {
				for _, want := range []string{
					fmt.Sprintf(`data-reading-repository="%d"`, item.ID),
					fmt.Sprintf(`id="reading-note-%d" class="project-reading-note" data-reading-note role="status" aria-live="polite" hidden></p>`, item.ID),
				} {
					if !strings.Contains(body, want) {
						t.Errorf("project %d missing reading-state contract %q", item.ID, want)
					}
				}
				for _, want := range []string{
					`type="button"`, `aria-pressed="false"`,
					fmt.Sprintf(`aria-describedby="reading-local-note reading-note-%d"`, item.ID),
					`data-reading-mark-read="` + l.Text("feed.mark_read") + `"`,
					`data-reading-mark-unread="` + l.Text("feed.mark_unread") + `"`,
					`data-reading-save-error="` + l.Text("feed.reading_save_error") + `"`,
				} {
					if !strings.Contains(buttons[index], want) {
						t.Errorf("project %d reading button missing %q", item.ID, want)
					}
				}
				// With scripts disabled, the uninitialized local-only button is
				// hidden and normal server-rendered navigation is still usable.
				if !regexp.MustCompile(`\shidden(?:\s|>)`).MatchString(buttons[index]) {
					t.Fatal("a nonfunctional reading button must not appear without JavaScript")
				}
				for _, want := range []string{
					fmt.Sprintf(`href="/repositories/%d?date=2026-08-30&lang=%s&return_to=`, item.ID, locale),
					`href="https://github.com/` + item.FullName + `" target="_blank" rel="noopener noreferrer"`,
				} {
					if !strings.Contains(body, want) {
						t.Errorf("no-script navigation missing %q", want)
					}
				}
			}
		})
	}
}

package web

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLibraryHasThreeTabsAndOneAddEntryInBothLanguages(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		for _, test := range []struct {
			query          string
			newOnly, focus bool
		}{
			{"", true, false}, {"view=all", false, false}, {"view=daily", true, false},
			{"view=all&new=0", false, false}, {"view=daily&new=0", false, false},
			{"view=focus", false, true}, {"focus=1", false, true},
		} {
			t.Run(locale+"/"+test.query, func(t *testing.T) {
				queryer := populatedFake()
				body := html.UnescapeString(request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories?lang="+locale+"&"+test.query).Body.String())
				filter := queryer.lastRepositoryQuery
				if filter.OnlyNew != test.newOnly || filter.OnlyFocus != test.focus {
					t.Fatalf("wrong initial scope: %+v", filter)
				}
				links := libraryViewURLs(t, body)
				if links[0].Query().Get("view") != "daily" || links[0].Query().Get("new") != "1" || links[0].Query().Get("focus") != "0" ||
					links[1].Query().Get("view") != "all" || links[1].Query().Get("new") != "0" || links[1].Query().Get("focus") != "0" ||
					links[2].Query().Get("view") != "focus" || links[2].Query().Get("new") != "0" || links[2].Query().Get("focus") != "1" {
					t.Fatalf("tabs do not have independent, explicit defaults: %v", links)
				}
				nav := strings.SplitN(strings.SplitN(body, `<nav class="library-views"`, 2)[1], "</nav>", 2)[0]
				l := newLocalizer(locale)
				wantActive := l.Text("daily.today")
				if test.focus {
					wantActive = l.Text("ui.my_watchlist")
				} else if !test.newOnly {
					wantActive = l.Text("ui.all_library")
				}
				active := regexp.MustCompile(`<a\b[^>]*aria-current="page"[^>]*>.*?</a>`).FindAllString(nav, -1)
				if len(active) != 1 || !strings.Contains(active[0], ">"+wantActive) {
					t.Fatalf("wrong tab highlighted: %s", nav)
				}
				for _, label := range []string{l.Text("daily.today"), l.Text("ui.all_library"), l.Text("ui.my_watchlist")} {
					if !strings.Contains(nav, ">"+label) {
						t.Fatalf("missing %q tab: %s", label, nav)
					}
				}
				entry := regexp.MustCompile(`<a\b[^>]*class="[^"]*header-add[^"]*"[^>]*>.*?</a>`).FindAllString(body, -1)
				if len(entry) != 1 || !strings.Contains(entry[0], `href="/watch/new"`) || !strings.Contains(entry[0], l.Text("approved.add_project")) {
					t.Fatalf("missing unique Add project entry: %v", entry)
				}
				canonical, _ := url.Parse(navigationAttribute(t, body, `data-list-url="([^"]+)"`))
				wantView := "all"
				if test.newOnly {
					wantView = "daily"
				} else if test.focus {
					wantView = "focus"
				}
				if canonical.Query().Get("view") != wantView {
					t.Fatalf("return context view = %q, want %q", canonical.Query().Get("view"), wantView)
				}
			})
		}
	}
}

func TestLibraryWatchlistTabShowsOlderProjectsAndAllTabRestoresItsDefault(t *testing.T) {
	item := RepositoryMetric{ID: 101, FullName: "owner/older-followed-project", IsFocus: true, FirstSeenAt: mustDate("2026-08-01")}
	queryer := &datedLibraryQueryer{fakeQueryer: populatedFake(), lastAvailable: mustDate("2026-08-30"), project: &item}
	handler := dailyViewHandler(t, queryer, mustTime("2026-09-08T01:00:00Z"))
	initial := request(t, handler, "/repositories?view=daily&new=1&date=2026-09-08&tag=skills&q=agent&period=30d&sort=growth_rate&page=1&size=6&lang=en")
	focusURL := libraryViewURLs(t, initial.Body.String())[2]
	focus := request(t, handler, focusURL.String())
	if !strings.Contains(focus.Body.String(), item.FullName) || !queryer.lastRepositoryQuery.OnlyFocus || queryer.lastRepositoryQuery.OnlyNew {
		t.Fatal("switching to My watchlist hid an older monitored project behind the daily filter")
	}
	allURL := libraryViewURLs(t, focus.Body.String())[1]
	for _, link := range []*url.URL{focusURL, allURL} {
		if link.Query().Has("cursor") || link.Query().Has("page") {
			t.Fatal("tab switch retained stale pagination state")
		}
		for key, want := range map[string]string{"date": "2026-09-08", "tag": "skills", "q": "agent", "period": "30d", "sort": "growth_rate", "size": "6", "lang": "en"} {
			if link.Query().Get(key) != want {
				t.Fatalf("tab switch lost %s: %s", key, link)
			}
		}
	}
	request(t, handler, allURL.String())
	if queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.OnlyFocus {
		t.Fatal("My watchlist's scope leaked into the All projects entry")
	}
}

func TestLibraryScopeAndFilterSubmissionKeepsChoiceThroughNumberedPageAndDetail(t *testing.T) {
	for _, scope := range []struct {
		view           string
		newOnly, focus bool
	}{
		{view: "daily", newOnly: true},
		{view: "all"},
		{view: "focus", focus: true},
	} {
		t.Run(scope.view, func(t *testing.T) {
			queryer := populatedFake()
			queryer.repositories.Total = 13
			handler := newTestHandler(t, queryer)
			body := request(t, handler, "/repositories?view="+scope.view+"&size=6").Body.String()
			if !strings.Contains(body, `name="view" value="`+scope.view+`"`) {
				t.Fatal("filter form lost its tab")
			}
			newControls := regexp.MustCompile(`<input\b[^>]*\bname="new"[^>]*>`).FindAllString(body, -1)
			wantNew := "0"
			if scope.newOnly {
				wantNew = "1"
			}
			if len(newControls) != 1 || !strings.Contains(newControls[0], `type="hidden"`) || !strings.Contains(newControls[0], `value="`+wantNew+`"`) {
				t.Fatalf("scope must submit one explicit new value: %v", newControls)
			}
			values := url.Values{"view": {scope.view}, "new": {wantNew}, "date": {"2026-08-30"}, "q": {"中文 skills"}, "tag": {"文档"}, "period": {"30d"}, "sort": {"stars"}, "size": {"6"}, "lang": {localeChinese}}
			if scope.focus {
				values.Set("focus", "1")
			}
			searched := request(t, handler, queryPath("/repositories", values))
			if queryer.lastRepositoryQuery.OnlyNew != scope.newOnly || queryer.lastRepositoryQuery.OnlyFocus != scope.focus {
				t.Fatalf("form reapplied defaults: %+v", queryer.lastRepositoryQuery)
			}
			searchedBody := html.UnescapeString(searched.Body.String())
			nextURL := navigationAttribute(t, searchedBody, `href="([^"]+)" rel="next"`)
			parsedNext, _ := url.Parse(nextURL)
			if parsedNext.Query().Get("page") != "2" || parsedNext.Query().Get("size") != "6" || parsedNext.Query().Has("cursor") || len(parsedNext.Query()["new"]) != 1 {
				t.Fatalf("numbered pagination lost or duplicated form state: %s", parsedNext)
			}
			next := request(t, handler, nextURL)
			if queryer.lastRepositoryQuery.OnlyNew != scope.newOnly || queryer.lastRepositoryQuery.OnlyFocus != scope.focus || queryer.lastRepositoryQuery.Offset != 6 || queryer.lastRepositoryQuery.Limit != 6 || queryer.lastRepositoryQuery.AfterID != nil || queryer.lastRepositoryQuery.Tag != "文档" || queryer.lastRepositoryQuery.Search != "中文 skills" {
				t.Fatalf("page changed user choice: %+v", queryer.lastRepositoryQuery)
			}
			detailURL := navigationAttribute(t, next.Body.String(), `href="([^"]+)" data-repository-detail`)
			detail := request(t, handler, detailURL)
			back := navigationAttribute(t, detail.Body.String(), `href="([^"]+)" data-library-return`)
			request(t, handler, back)
			if queryer.lastRepositoryQuery.OnlyNew != scope.newOnly || queryer.lastRepositoryQuery.OnlyFocus != scope.focus || queryer.lastRepositoryQuery.Offset != 6 || queryer.lastRepositoryQuery.Limit != 6 || queryer.lastRepositoryQuery.AfterID != nil {
				t.Fatalf("detail return reset the selected scope: %+v", queryer.lastRepositoryQuery)
			}
		})
	}
}

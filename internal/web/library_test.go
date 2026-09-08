package web

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestLibraryHasTwoTabsAndOneImportEntryInBothLanguages(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		for _, test := range []struct {
			query          string
			newOnly, focus bool
		}{
			{"", true, false}, {"view=all", true, false}, {"view=daily", true, false},
			{"view=all&new=0", false, false}, {"view=daily&new=0", false, false},
			{"view=focus", false, true}, {"focus=1", false, true},
		} {
			t.Run(locale+"/"+test.query, func(t *testing.T) {
				queryer := populatedFake()
				body := request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories?lang="+locale+"&"+test.query).Body.String()
				filter := queryer.lastRepositoryQuery
				if filter.OnlyNew != test.newOnly || filter.OnlyFocus != test.focus {
					t.Fatalf("wrong initial scope: %+v", filter)
				}
				links := libraryViewURLs(t, body)
				if links[0].Query().Get("view") != "all" || links[0].Query().Get("new") != "1" || links[0].Query().Get("focus") != "0" || links[1].Query().Get("view") != "focus" || links[1].Query().Get("new") != "0" || links[1].Query().Get("focus") != "1" {
					t.Fatalf("tabs do not have independent, explicit defaults: %v", links)
				}
				nav := strings.SplitN(strings.SplitN(body, `<nav class="library-views"`, 2)[1], "</nav>", 2)[0]
				l := newLocalizer(locale)
				wantActive := l.Text("ui.all_library")
				if test.focus {
					wantActive = l.Text("ui.my_watchlist")
				}
				if strings.Count(nav, `aria-current="page"`) != 1 || !strings.Contains(nav, `aria-current="page">`+wantActive+`</a>`) || strings.Contains(nav, l.Text("daily.today")) {
					t.Fatalf("wrong tab highlighted or third daily tab retained: %s", nav)
				}
				entry := regexp.MustCompile(`<a\b[^>]*class="[^"]*library-add[^"]*"[^>]*>.*?</a>`).FindAllString(body, -1)
				if len(entry) != 1 || !strings.Contains(entry[0], `href="/watch/new"`) || !strings.Contains(entry[0], l.Text("library.import")) {
					t.Fatalf("missing unique Import entry: %v", entry)
				}
				canonical, _ := url.Parse(navigationAttribute(t, body, `data-list-url="([^"]+)"`))
				if canonical.Query().Get("view") == "daily" {
					t.Fatal("new return context emitted the obsolete daily tab")
				}
			})
		}
	}
}

func TestLibraryWatchlistTabShowsOlderProjectsAndAllTabRestoresItsDefault(t *testing.T) {
	item := RepositoryMetric{ID: 101, FullName: "owner/older-followed-project", IsFocus: true, FirstSeenAt: mustDate("2026-08-01")}
	queryer := &datedLibraryQueryer{fakeQueryer: populatedFake(), lastAvailable: mustDate("2026-08-30"), project: &item}
	handler := dailyViewHandler(t, queryer, mustTime("2026-09-08T01:00:00Z"))
	initial := request(t, handler, "/repositories?view=all&new=1&date=2026-09-08&tag=skills&q=agent&period=30d&sort=growth_rate&cursor=2s&lang=en")
	focusURL := libraryViewURLs(t, initial.Body.String())[1]
	focus := request(t, handler, focusURL.String())
	if !strings.Contains(focus.Body.String(), item.FullName) || !queryer.lastRepositoryQuery.OnlyFocus || queryer.lastRepositoryQuery.OnlyNew {
		t.Fatal("switching to My watchlist hid an older monitored project behind the daily filter")
	}
	allURL := libraryViewURLs(t, focus.Body.String())[0]
	for _, link := range []*url.URL{focusURL, allURL} {
		if link.Query().Has("cursor") {
			t.Fatal("tab switch retained a stale cursor")
		}
		for key, want := range map[string]string{"date": "2026-09-08", "tag": "skills", "q": "agent", "period": "30d", "sort": "growth_rate", "lang": "en"} {
			if link.Query().Get(key) != want {
				t.Fatalf("tab switch lost %s: %s", key, link)
			}
		}
	}
	request(t, handler, allURL.String())
	if !queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.OnlyFocus {
		t.Fatal("My watchlist's automatic new=0 leaked into the All projects entry")
	}
}

func TestLibraryNativeCheckboxSubmissionKeepsChoiceThroughSearchPageAndDetail(t *testing.T) {
	for _, focus := range []bool{false, true} {
		for _, checked := range []bool{false, true} {
			queryer := populatedFake()
			queryer.repositories.HasMore, queryer.repositories.NextCursor = true, "2t"
			handler := newTestHandler(t, queryer)
			view := "all"
			if focus {
				view = "focus"
			}
			body := request(t, handler, "/repositories?view="+view).Body.String()
			if !strings.Contains(body, `name="view" value="`+view+`"`) {
				t.Fatal("filter form lost its tab")
			}
			values := url.Values{"view": {view}, "date": {"2026-08-30"}, "q": {"中文 skills"}, "tag": {"文档"}, "period": {"30d"}, "sort": {"stars"}, "lang": {localeChinese}}
			if focus {
				values.Set("focus", "1")
			}
			// Emulate successful controls in DOM order: unchecked boxes are not
			// submitted, while the later hidden false input always is. No JS.
			controls := regexp.MustCompile(`<input\b[^>]*\bname="new"[^>]*>`).FindAllString(body, -1)
			if len(controls) != 2 || !strings.Contains(controls[0], `type="checkbox"`) || !strings.Contains(controls[1], `type="hidden"`) {
				t.Fatalf("checkbox/false fallback order invalid: %v", controls)
			}
			for _, control := range controls {
				if strings.Contains(control, `type="checkbox"`) && !checked {
					continue
				}
				values.Add("new", navigationAttribute(t, control, `value="([^"]+)"`))
			}
			searched := request(t, handler, queryPath("/repositories", values))
			if queryer.lastRepositoryQuery.OnlyNew != checked || queryer.lastRepositoryQuery.OnlyFocus != focus {
				t.Fatalf("form reapplied defaults: %+v", queryer.lastRepositoryQuery)
			}
			searchedBody := html.UnescapeString(searched.Body.String())
			nextURL := navigationAttribute(t, searchedBody, `href="([^"]+)" rel="next"`)
			parsedNext, _ := url.Parse(nextURL)
			if len(parsedNext.Query()["new"]) != 1 {
				t.Fatal("pagination propagated duplicate form values")
			}
			next := request(t, handler, nextURL)
			if queryer.lastRepositoryQuery.OnlyNew != checked || queryer.lastRepositoryQuery.OnlyFocus != focus || queryer.lastRepositoryQuery.AfterID == nil || queryer.lastRepositoryQuery.Tag != "文档" || queryer.lastRepositoryQuery.Search != "中文 skills" {
				t.Fatalf("page changed user choice: %+v", queryer.lastRepositoryQuery)
			}
			detailURL := navigationAttribute(t, next.Body.String(), `href="([^"]+)" data-repository-detail`)
			detail := request(t, handler, detailURL)
			back := navigationAttribute(t, detail.Body.String(), `href="([^"]+)" data-library-return`)
			request(t, handler, back)
			if queryer.lastRepositoryQuery.OnlyNew != checked || queryer.lastRepositoryQuery.OnlyFocus != focus || queryer.lastRepositoryQuery.AfterID == nil {
				t.Fatalf("detail return reset the selected scope: %+v", queryer.lastRepositoryQuery)
			}
		}
	}
}

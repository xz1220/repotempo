package web

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

type datedLibraryQueryer struct {
	*fakeQueryer
	lastAvailable time.Time
	project       *RepositoryMetric
}

func (queryer *datedLibraryQueryer) ListRepositoryTrends(_ context.Context, filter RepositoryQuery) (RepositoryPage, error) {
	queryer.lastRepositoryQuery = filter
	selected := filter.AsOf
	if selected.IsZero() {
		selected = queryer.lastAvailable
	}
	page := RepositoryPage{Items: []RepositoryMetric{}, Coverage: ComparisonCoverage{AsOfDate: selected, BaselineDate: selected.AddDate(0, 0, -filter.WindowDays)}}
	if queryer.project == nil {
		return page, nil
	}
	page.Coverage.ScopeCount = 1
	if !filter.OnlyNew || queryer.project.FirstSeenAt.Format("2006-01-02") == selected.Format("2006-01-02") {
		page.Items = []RepositoryMetric{*queryer.project}
		page.Total = 1
	}
	return page, nil
}

func dailyViewHandler(t *testing.T, queryer Queryer, now time.Time) http.Handler {
	t.Helper()
	handler, err := New(queryer, Options{Now: func() time.Time { return now }, Location: time.UTC, Locale: localeChinese})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func libraryViewURLs(t *testing.T, body string) []*url.URL {
	t.Helper()
	parts := strings.SplitN(body, `<nav class="library-views"`, 2)
	if len(parts) != 2 {
		t.Fatal("missing library views")
	}
	nav := strings.SplitN(parts[1], "</nav>", 2)[0]
	matches := regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(nav, -1)
	if len(matches) != 3 {
		t.Fatalf("view count %d, want three", len(matches))
	}
	result := make([]*url.URL, 0, len(matches))
	for _, match := range matches {
		parsed, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, parsed)
	}
	return result
}

func TestLibraryDefaultUsesActualShanghaiTodayWithoutFallingBackToOldData(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "old data only", true: "empty database"}[empty], func(t *testing.T) {
			project := RepositoryMetric{ID: 1, FullName: "owner/older-project", FirstSeenAt: mustDate("2026-08-30"), LastObservedStars: int64Pointer(100)}
			queryer := &datedLibraryQueryer{fakeQueryer: &fakeQueryer{}, lastAvailable: mustDate("2026-08-30")}
			if !empty {
				queryer.project = &project
			}
			// This is already September 8 in Shanghai, but still September 7 UTC.
			handler := dailyViewHandler(t, queryer, mustTime("2026-09-07T16:10:00Z"))
			response := request(t, handler, "/repositories?lang=zh-CN")
			got := queryer.lastRepositoryQuery
			if response.Code != http.StatusOK || !got.OnlyNew || got.AsOf.Format("2006-01-02") != "2026-09-08" || got.WindowDays != 1 || got.Sort != "stars" || got.Limit != 20 {
				t.Fatalf("wrong default query: %+v, status %d", got, response.Code)
			}
			body := html.UnescapeString(response.Body.String())
			if !strings.Contains(body, "2026-09-08 没有符合条件的新入库项目") || !strings.Contains(body, "当天新入库") || strings.Contains(body, project.FullName) {
				t.Fatal("daily empty state substituted older projects for today")
			}
			if !strings.Contains(body, `href="/repositories?lang=zh-CN&new=0&view=all"`) {
				t.Fatal("daily empty state lacks a full-library escape")
			}
			all := request(t, handler, "/repositories?lang=zh-CN&new=0&view=all")
			if queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.Sort != "velocity" || queryer.lastRepositoryQuery.WindowDays != 7 {
				t.Fatal("full library did not restore its defaults")
			}
			if !empty && !strings.Contains(all.Body.String(), project.FullName) {
				t.Fatal("full-library escape hid the previously monitored project")
			}
		})
	}
}

func TestLibraryExplicitLegacyFiltersRetainTheirScopeAndOrdering(t *testing.T) {
	for _, test := range []struct {
		query          string
		newOnly, focus bool
		period         int
		sort, date     string
	}{
		{"", true, false, 1, "stars", "2026-09-08"},
		{"lang=en", true, false, 1, "stars", "2026-09-08"},
		{"view=daily", true, false, 1, "stars", "2026-09-08"},
		{"view=all", false, false, 7, "velocity", ""},
		{"new=0", false, false, 7, "velocity", ""},
		{"focus=1", false, true, 7, "velocity", ""},
		{"view=focus", false, true, 7, "velocity", ""},
		{"sort=delta", false, false, 7, "delta", ""},
		{"sort=slowdown&period=30d&date=2026-08-30", false, false, 30, "slowdown", "2026-08-30"},
		{"period=1d", false, false, 1, "velocity", ""},
		{"date=2026-08-30", false, false, 7, "velocity", "2026-08-30"},
		{"topic=coding-agents", false, false, 7, "velocity", ""},
		{"q=agent", false, false, 7, "velocity", ""},
		{"q=", false, false, 7, "velocity", ""},
		{"source=github_trending", false, false, 7, "velocity", ""},
		{"status=paused", false, false, 7, "velocity", ""},
		{"cursor=2s", false, false, 7, "velocity", ""},
		{"new=1&date=2026-08-30", true, false, 1, "stars", "2026-08-30"},
		{"new=1&focus=1&period=30d&sort=growth_rate", true, true, 30, "growth_rate", "2026-09-08"},
		{"view=daily&new=0", false, false, 7, "velocity", ""},
		{"view=all&new=1", true, false, 1, "stars", "2026-09-08"},
	} {
		t.Run(test.query, func(t *testing.T) {
			queryer := populatedFake()
			response := request(t, dailyViewHandler(t, queryer, mustTime("2026-09-08T01:00:00Z")), "/repositories?"+test.query)
			got := queryer.lastRepositoryQuery
			date := ""
			if !got.AsOf.IsZero() {
				date = got.AsOf.Format("2006-01-02")
			}
			if response.Code != http.StatusOK || got.OnlyNew != test.newOnly || got.OnlyFocus != test.focus || got.WindowDays != test.period || got.Sort != test.sort || date != test.date {
				t.Fatalf("scope changed: %+v, date=%s, status=%d", got, date, response.Code)
			}
		})
	}
}

func TestLibraryTabsChangeScopeWithoutCarryingTheNewFilterIntoWatchlist(t *testing.T) {
	queryer := &datedLibraryQueryer{fakeQueryer: &fakeQueryer{}, lastAvailable: mustDate("2026-08-30")}
	handler := dailyViewHandler(t, queryer, mustTime("2026-09-08T01:00:00Z"))
	response := request(t, handler, "/repositories?lang=zh-CN")
	links := libraryViewURLs(t, response.Body.String())
	for index, mode := range []string{"daily", "all", "focus"} {
		values := links[index].Query()
		if values.Get("view") != mode || values.Has("cursor") || values.Get("lang") != localeChinese {
			t.Fatalf("invalid %s tab URL: %s", mode, links[index])
		}
		if index == 0 && values.Get("new") != "1" {
			t.Fatal("daily tab did not select additions")
		}
		if index > 0 && values.Get("new") != "0" {
			t.Fatal("all/watchlist tab retained additions filter")
		}
		request(t, handler, links[index].String())
		got := queryer.lastRepositoryQuery
		if index > 0 && (got.OnlyNew || got.WindowDays != 7 || got.Sort != "velocity") {
			t.Fatalf("%s tab has wrong defaults: %+v", mode, got)
		}
		if got.OnlyFocus != (index == 2) {
			t.Fatalf("%s tab focus=%v", mode, got.OnlyFocus)
		}
	}
	response = request(t, handler, "/repositories?view=daily&date=2026-08-30&topic=coding-agents&q=agent&period=30d&sort=growth_rate&source=github_trending&cursor=2s&lang=en")
	if !strings.Contains(response.Body.String(), "New on 2026-08-30") {
		t.Fatal("historical additions tab still claims today")
	}
	links = libraryViewURLs(t, response.Body.String())
	for _, link := range links {
		for key, expected := range map[string]string{"date": "2026-08-30", "topic": "coding-agents", "q": "agent", "period": "30d", "sort": "growth_rate", "source": "github_trending", "lang": "en"} {
			if link.Query().Get(key) != expected {
				t.Fatalf("tab lost explicit %s: %s", key, link)
			}
		}
	}
}

func TestDailyLibraryPaginationAndFilterSubmissionPreserveIntent(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.HasMore, queryer.repositories.NextCursor = true, "2t"
	handler := dailyViewHandler(t, queryer, mustTime("2026-08-30T01:00:00Z"))
	response := request(t, handler, "/repositories?lang=en")
	body := html.UnescapeString(response.Body.String())
	if !strings.Contains(body, `href="/repositories?cursor=2t&date=2026-08-30&lang=en&new=1"`) || !strings.Contains(body, `name="view" value="all"`) {
		t.Fatal("daily view missing pagination state or explicit form base view")
	}
	request(t, handler, "/repositories?cursor=2t&date=2026-08-30&lang=en&new=1")
	if !queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.Sort != "stars" || queryer.lastRepositoryQuery.WindowDays != 1 {
		t.Fatal("next page silently became all projects")
	}
	request(t, handler, "/repositories?view=all&date=2026-08-30&period=1d&sort=stars&topic=coding-agents&new=1")
	if !queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.TopicSlug != "coding-agents" {
		t.Fatal("checked new filter lost intent")
	}
	request(t, handler, "/repositories?view=all&date=2026-08-30&period=1d&sort=stars&topic=coding-agents")
	if queryer.lastRepositoryQuery.OnlyNew {
		t.Fatal("unchecking new filter reapplied the daily default")
	}
	request(t, handler, "/repositories?view=all")
	if queryer.lastRepositoryQuery.OnlyNew {
		t.Fatal("a blank full-library form reapplied the daily default")
	}
}

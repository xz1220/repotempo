package web

import (
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func populatedRadar(fast RepositoryMetric) RadarOverview {
	fast.FirstSeenAt = mustDate("2026-08-20")
	steady := RepositoryMetric{ID: 102, FullName: "acme/steady", HTMLURL: "https://github.com/acme/steady", Description: "A stable AI coding assistant.", PrimaryLanguage: "Go", CurrentStars: int64Pointer(2000), BaselineStars: int64Pointer(2000), StarDelta: int64Pointer(0), GrowthRate: float64Pointer(0), DailyVelocity: float64Pointer(0)}
	slowing := RepositoryMetric{ID: 103, FullName: "acme/slowing", HTMLURL: "https://github.com/acme/slowing", Description: "An agent whose growth has slowed.", PrimaryLanguage: "Python", CurrentStars: int64Pointer(350), BaselineStars: int64Pointer(300), StarDelta: int64Pointer(50), GrowthRate: float64Pointer(16.7), DailyVelocity: float64Pointer(7.14)}
	newProject := RepositoryMetric{ID: 104, FullName: "acme/new-agent", HTMLURL: "https://github.com/acme/new-agent", Description: "A newly discovered AI research agent.", CurrentStars: int64Pointer(100), IsNew: true, FirstSeenAt: mustDate("2026-08-30")}
	return RadarOverview{
		Filter: RepositoryQuery{WindowDays: 7}, AsOf: mustDate("2026-08-30"), BaselineDate: mustDate("2026-08-23"), PreviousDate: mustDate("2026-08-16"),
		Coverage:        RadarCoverage{ComparisonCoverage: ComparisonCoverage{AsOfDate: mustDate("2026-08-30"), BaselineDate: mustDate("2026-08-23"), ScopeCount: 6, ObservedCount: 5, ComparableCount: 4, NewCount: 1}, UpCount: 2, FlatCount: 1, DownCount: 1, MissingCount: 1, StaleCount: 1, MomentumComparableCount: 3, SlowingCount: 1},
		Fastest:         []RadarRepository{{RepositoryMetric: fast}},
		Slowest:         []RadarRepository{{RepositoryMetric: steady}},
		FallingBehind:   []RadarRepository{{RepositoryMetric: slowing, PreviousDelta: int64Pointer(200), MomentumChange: int64Pointer(-150)}},
		NewRepositories: []RadarRepository{{RepositoryMetric: newProject}},
		History: []RadarHistoryPoint{
			{Date: mustDate("2026-08-23"), Stars: int64Pointer(16000), Index: float64Pointer(100), ObservedCount: 4, CohortCount: 4},
			{Date: mustDate("2026-08-24"), Stars: nil, Index: nil, ObservedCount: 3, CohortCount: 4},
			{Date: mustDate("2026-08-29"), Stars: int64Pointer(16300), Index: float64Pointer(101.875), ObservedCount: 4, CohortCount: 4},
			{Date: mustDate("2026-08-30"), Stars: int64Pointer(16480), Index: float64Pointer(103), ObservedCount: 4, CohortCount: 4},
		},
		Topics: []TopicRef{{Slug: "coding-agents", Name: "Coding agents"}, {Slug: "research-agents", Name: "Research agents"}},
	}
}

func TestRadarHomeShowsThreeEvidenceBoardsAndAccessibleCharts(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			handler := newTestHandlerWithLocale(t, queryer, locale)
			response := request(t, handler, "/?period=7d&topic=coding-agents&focus=1&date=2026-08-30&lang="+locale)
			if response.Code != http.StatusOK {
				t.Fatalf("home status = %d", response.Code)
			}
			got := queryer.lastRadarQuery
			if got.WindowDays != 7 || got.TopicSlug != "coding-agents" || !got.OnlyFocus || got.AsOf.Format("2006-01-02") != "2026-08-30" {
				t.Fatalf("overview filters lost: %#v", got)
			}
			body := html.UnescapeString(response.Body.String())
			localizer := newLocalizer(locale)
			for _, key := range []string{"ui.fastest", "ui.slowest", "ui.slowdown", "ui.chart_help", "ui.direction_title", "ui.methodology_help"} {
				if !strings.Contains(body, localizer.Text(key)) {
					t.Errorf("home missing %s", key)
				}
			}
			for _, expected := range []string{`href="/repositories/101"`, `href="/repositories/102"`, `href="/repositories/103"`, `aria-label="2 growing, 1 steady, 1 declining"`} {
				if locale == localeChinese && strings.Contains(expected, "growing") {
					expected = `aria-label="增长 2 个、持平 1 个、减少 1 个"`
				}
				if !strings.Contains(body, expected) {
					t.Errorf("home missing %s", expected)
				}
			}
			if strings.Count(body, `class="surface leaderboard"`) != 3 {
				t.Fatal("expected exactly three leaderboards")
			}
			if strings.Count(body, `class="chart-line"`) != 2 {
				t.Fatal("incomplete history day should split the line")
			}
			if !strings.Contains(body, `class="line-chart"`) || !strings.Contains(body, `class="direction-donut"`) || !strings.Contains(body, `role="img"`) {
				t.Fatal("trend and distribution SVG charts missing")
			}
			if !strings.Contains(body, "+200 → +50") {
				t.Fatal("slowing project must show both positive period gains")
			}
			newProjects := `/repositories?date=2026-08-30&focus=1&lang=` + locale + `&new=1&period=1d&sort=stars&topic=coding-agents`
			if !strings.Contains(body, `href="`+newProjects+`"`) || strings.Contains(body, `href="/discoveries`) {
				t.Fatal("dashboard new-project link should reach the filtered library with its date and scope")
			}
			if !strings.Contains(body, `class="metric-neutral">0</strong>`) {
				t.Fatal("observed zero growth was not shown as zero")
			}
			if strings.Contains(body, "NaN") || strings.Contains(body, "+Inf") || strings.Contains(body, "#ZgotmplZ") {
				t.Fatal("invalid chart value was rendered")
			}
		})
	}
}

func TestRadarNewUserStatesKeepAddingInTheProjectLibrary(t *testing.T) {
	handler := newTestHandler(t, &fakeQueryer{})
	for _, path := range []string{"/", "/repositories"} {
		response := request(t, handler, path)
		if response.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, response.Code)
		}
		body := response.Body.String()
		wantAddCount := 0
		if path == "/repositories" {
			wantAddCount = 1
		}
		if count := strings.Count(body, `href="/watch/new"`); count != wantAddCount {
			t.Fatalf("%s has %d add entries, want %d", path, count, wantAddCount)
		}
		if !strings.Contains(body, `href="/repositories"`) || strings.Contains(body, `href="/discoveries"`) {
			t.Fatalf("%s should direct browsing through the project library", path)
		}
		if strings.Contains(body, `class="chart-line"`) || strings.Contains(body, `class="leaderboard-list"`) {
			t.Fatalf("%s fabricated trend evidence", path)
		}
	}
	body := request(t, handler, "/").Body.String()
	if strings.Count(body, "No comparable projects in this group") != 3 {
		t.Fatal("empty boards must explain the lack of comparisons")
	}
	if !strings.Contains(body, "Waiting for comparable history") {
		t.Fatal("new user chart state is missing")
	}
}

func TestLibraryKeepsMissingEvidenceSeparateFromObservedZero(t *testing.T) {
	queryer := populatedFake()
	zero := queryer.radar.Slowest[0].RepositoryMetric
	lastDate := mustDate("2026-08-29")
	missing := RepositoryMetric{ID: 105, FullName: "acme/missing", Description: "Awaiting today's observation", LastObservedStars: int64Pointer(999), LastObservedAt: &lastDate, IsStale: true, FirstSeenAt: mustDate("2026-08-20")}
	queryer.repositories.Items = []RepositoryMetric{zero, missing}
	queryer.repositories.Total = 2
	body := html.UnescapeString(request(t, newTestHandler(t, queryer), "/repositories").Body.String())
	for _, want := range []string{"acme/steady", "acme/missing", "999", "Awaiting observation", "Last observed 2026-08-29", `class="numeric metric-neutral">0`, `class="numeric metric-neutral">N/A`} {
		if !strings.Contains(body, want) {
			t.Errorf("library missing evidence state %q", want)
		}
	}
}

func TestLibraryPaginationPreservesFocusAndFreezesObservationDate(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Total, queryer.repositories.HasMore, queryer.repositories.NextCursor = 120, true, "2t"
	body := html.UnescapeString(request(t, newTestHandler(t, queryer), "/repositories?focus=1&q=agent&topic=research-agents&period=30d&sort=stars&new=1&cursor=2s&lang=zh-CN").Body.String())
	if !queryer.lastRepositoryQuery.OnlyFocus {
		t.Fatal("focus filter not sent to storage")
	}
	for _, want := range []string{
		`href="/repositories?cursor=2t&date=2026-08-30&focus=1&lang=zh-CN&new=1&period=30d&q=agent&sort=stars&topic=research-agents"`,
		`href="/repositories?date=2026-08-30&focus=1&lang=zh-CN&new=1&period=30d&q=agent&sort=stars&topic=research-agents"`,
		`name="focus" value="1"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("pagination or filter form lost %q", want)
		}
	}
	parts := strings.SplitN(body, `<nav class="library-views"`, 2)
	if len(parts) != 2 {
		t.Fatal("library view switch is missing")
	}
	navigation := strings.SplitN(parts[1], "</nav>", 2)[0]
	links := regexp.MustCompile(`href="([^"]+)"`).FindAllStringSubmatch(navigation, -1)
	if len(links) != 2 {
		t.Fatalf("library view links = %d, want All and My watchlist", len(links))
	}
	for index, link := range links {
		location, err := url.Parse(link[1])
		if err != nil || location.Path != "/repositories" || location.Query().Has("cursor") {
			t.Fatalf("invalid library view URL %q", link[1])
		}
		for key, want := range map[string]string{"date": "2026-08-30", "topic": "research-agents", "new": "1", "sort": "stars", "q": "agent", "lang": "zh-CN", "period": "30d"} {
			if got := location.Query().Get(key); got != want {
				t.Errorf("view %d lost %s: got %q, want %q", index, key, got, want)
			}
		}
		wantFocus := ""
		if index == 1 {
			wantFocus = "1"
		}
		if location.Query().Get("focus") != wantFocus {
			t.Errorf("view %d focus = %q, want %q", index, location.Query().Get("focus"), wantFocus)
		}
	}
}

func TestLibraryNewFilterIsVisibleReversibleAndExplainsFirstSeenDate(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		queryer := populatedFake()
		queryer.repositories.Items = []RepositoryMetric{queryer.radar.NewRepositories[0].RepositoryMetric}
		queryer.repositories.Total = 1
		handler := newTestHandlerWithLocale(t, queryer, locale)
		for _, selected := range []bool{true, false} {
			path := "/repositories?date=2026-08-30&topic=research-agents&lang=" + locale
			if selected {
				path += "&new=1"
			}
			response := request(t, handler, path)
			if response.Code != http.StatusOK || queryer.lastRepositoryQuery.OnlyNew != selected {
				t.Fatalf("new filter did not reach storage: status=%d query=%+v", response.Code, queryer.lastRepositoryQuery)
			}
			body := html.UnescapeString(response.Body.String())
			inputs := regexp.MustCompile(`<input\b[^>]*\bname="new"[^>]*>`).FindAllString(body, -1)
			if len(inputs) != 1 || !strings.Contains(inputs[0], `type="checkbox"`) || !strings.Contains(inputs[0], `value="1"`) || strings.Contains(inputs[0], "checked") != selected {
				t.Fatalf("new filter should be one reversible checkbox: %v", inputs)
			}
			label, help := "New on this date only", "Only projects first added on the observation date. This is not their GitHub creation date."
			if locale == localeChinese {
				label, help = "仅看当天新入库", "仅显示在所选观测日期首次入库的项目，入库日期不等于 GitHub 创建日期。"
			}
			if !strings.Contains(body, label) || strings.Contains(body, help) != selected {
				t.Fatalf("locale %s did not explain the selected filter", locale)
			}
			if selected {
				l := newLocalizer(locale)
				mixedCount := l.Textf("ui.library_note", formatInt(queryer.repositories.Total), formatInt(queryer.repositories.Coverage.ComparableCount))
				if strings.Contains(body, mixedCount) || !strings.Contains(body, l.Textf("repositories.count", "1")) {
					t.Fatal("new-project count must not mix its total with the entire library's comparable population")
				}
			}
			for _, want := range []string{"acme/new-agent", "A newly discovered AI research agent.", `href="/repositories/104"`} {
				if !strings.Contains(body, want) {
					t.Errorf("filtered library missing %q", want)
				}
			}
		}
		request(t, handler, "/repositories/104")
		if queryer.lastRepositoryID != 104 {
			t.Fatal("new project detail target not routed")
		}
	}
}

func TestLegacyHomeSearchLinksStillReachTheProjectLibrary(t *testing.T) {
	handler := newTestHandler(t, populatedFake())
	for _, rawQuery := range []string{"new=1&lang=zh-CN", "q=agent&period=7d", "cursor=2s&focus=1"} {
		response := request(t, handler, "/?"+rawQuery)
		if response.Code != http.StatusFound {
			t.Fatalf("legacy URL did not redirect: %s", rawQuery)
		}
		location, err := url.Parse(response.Header().Get("Location"))
		if err != nil || location.Path != "/repositories" {
			t.Fatalf("invalid destination %q", response.Header().Get("Location"))
		}
		want, _ := url.ParseQuery(rawQuery)
		if location.Query().Encode() != want.Encode() {
			t.Fatal("legacy URL filters were lost")
		}
	}
}

func TestNewPagesKeepInternalErrorsAndHostileTextOutOfMarkup(t *testing.T) {
	queryer := populatedFake()
	queryer.radar.Fastest[0].FullName = `<script>alert("radar")</script>`
	queryer.repositories.Items[0].Description = `<script>alert("discover")</script>`
	handler := newTestHandler(t, queryer)
	for _, path := range []string{"/", "/repositories?new=1"} {
		body := request(t, handler, path).Body.String()
		if strings.Contains(body, `<script>alert(`) || !strings.Contains(body, "&lt;script&gt;") {
			t.Fatalf("%s did not escape hostile text", path)
		}
	}
	queryer.radarErr, queryer.repositoriesErr = errors.New("private-database-token"), errors.New("private-database-token")
	for _, path := range []string{"/", "/repositories?new=1", "/repositories"} {
		response := request(t, handler, path)
		if response.Code != http.StatusInternalServerError || strings.Contains(response.Body.String(), "private-database-token") {
			t.Fatalf("%s did not sanitize a query error", path)
		}
	}
}

func timePointer(value time.Time) *time.Time { return &value }

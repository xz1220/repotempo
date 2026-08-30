package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type fakeQueryer struct {
	dashboard           DashboardSummary
	dashboardErr        error
	repositories        RepositoryPage
	repositoriesErr     error
	repository          RepositoryDetail
	repositoryErr       error
	topics              TopicPage
	topicsErr           error
	topic               TopicDetail
	topicErr            error
	discoveries         DiscoverySummary
	discoveriesErr      error
	runs                RunsPage
	runsErr             error
	readyErr            error
	lastRepositoryQuery RepositoryQuery
	lastRepositoryID    int64
	lastTopicSlug       string
	lastExcludeLeader   bool
	lastRunLimit        int
	lastRunOffset       int
}

func (f *fakeQueryer) DashboardSummary(context.Context, time.Time) (DashboardSummary, error) {
	return f.dashboard, f.dashboardErr
}

func (f *fakeQueryer) ListRepositoryMetrics(_ context.Context, query RepositoryQuery) (RepositoryPage, error) {
	f.lastRepositoryQuery = query
	return f.repositories, f.repositoriesErr
}

func (f *fakeQueryer) GetRepositoryDetail(_ context.Context, id int64, _ time.Time) (RepositoryDetail, error) {
	f.lastRepositoryID = id
	return f.repository, f.repositoryErr
}

func (f *fakeQueryer) ListTopicMetrics(context.Context, time.Time) (TopicPage, error) {
	return f.topics, f.topicsErr
}

func (f *fakeQueryer) GetTopicDetail(_ context.Context, slug string, _ time.Time, excludeLeader bool) (TopicDetail, error) {
	f.lastTopicSlug = slug
	f.lastExcludeLeader = excludeLeader
	return f.topic, f.topicErr
}

func (f *fakeQueryer) DiscoverySummary(context.Context) (DiscoverySummary, error) {
	return f.discoveries, f.discoveriesErr
}

func (f *fakeQueryer) ListJobRuns(_ context.Context, limit, offset int) (RunsPage, error) {
	f.lastRunLimit = limit
	f.lastRunOffset = offset
	return f.runs, f.runsErr
}

func (f *fakeQueryer) Ready(context.Context) error {
	return f.readyErr
}

func TestMainRoutesRender(t *testing.T) {
	queryer := populatedFake()
	handler := newTestHandler(t, queryer)

	tests := []struct {
		path        string
		wantStatus  int
		wantContent string
	}{
		{path: "/", wantStatus: http.StatusOK, wantContent: "Monitoring summary"},
		{path: "/repositories", wantStatus: http.StatusOK, wantContent: "acme/radar"},
		{path: "/repositories/101", wantStatus: http.StatusOK, wantContent: "Valid history starts"},
		{path: "/topics", wantStatus: http.StatusOK, wantContent: "AI Agent"},
		{path: "/topics/ai-agent", wantStatus: http.StatusOK, wantContent: "Top repository share"},
		{path: "/discoveries", wantStatus: http.StatusOK, wantContent: "GitHub Search profiles"},
		{path: "/runs", wantStatus: http.StatusOK, wantContent: "Recent job runs"},
		{path: "/static/app.css", wantStatus: http.StatusOK, wantContent: "--accent:"},
		{path: "/static/app.js", wantStatus: http.StatusOK, wantContent: "data-nav-toggle"},
		{path: "/healthz", wantStatus: http.StatusOK, wantContent: "ok"},
		{path: "/readyz", wantStatus: http.StatusOK, wantContent: "ready"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, test.path)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", response.Code, test.wantStatus, response.Body.String())
			}
			if !strings.Contains(response.Body.String(), test.wantContent) {
				t.Fatalf("body does not contain %q: %s", test.wantContent, response.Body.String())
			}
		})
	}
}

func TestChineseLocaleRendersAllDashboardRoutes(t *testing.T) {
	queryer := populatedFake()
	queryer.dashboard.Warnings = []string{"Current snapshot coverage is temporarily unavailable."}
	handler := newTestHandlerWithLocale(t, queryer, localeChinese)

	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "监控概况"},
		{path: "/repositories", want: "项目指标"},
		{path: "/repositories/101", want: "有效历史起始日"},
		{path: "/topics", want: "主题指标"},
		{path: "/topics/ai-agent", want: "头部项目占比"},
		{path: "/discoveries", want: "GitHub Search 配置"},
		{path: "/runs", want: "最近任务运行"},
		{path: "/not-here", want: "页面不存在"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, test.path)
			if response.Code != http.StatusOK && response.Code != http.StatusNotFound {
				t.Fatalf("status = %d; body: %s", response.Code, response.Body.String())
			}
			body := response.Body.String()
			if !strings.Contains(body, test.want) {
				t.Fatalf("body does not contain %q: %s", test.want, body)
			}
			if !strings.Contains(body, `<html lang="zh-CN">`) {
				t.Fatalf("page language is not zh-CN: %s", body)
			}
		})
	}

	home := request(t, handler, "/").Body.String()
	for _, want := range []string{"当前快照覆盖率暂不可用。", "部分成功", "截至 2026-08-30"} {
		if !strings.Contains(home, want) {
			t.Errorf("Chinese overview does not contain %q", want)
		}
	}
}

func TestLanguageQueryPersistsCookieAndPreservesLocation(t *testing.T) {
	handler := newTestHandlerWithLocale(t, populatedFake(), localeEnglish)
	response := request(t, handler, "/repositories?q=acme+radar&topic=ai-agent&lang=zh-CN")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := response.Header().Get("Content-Language"); got != localeChinese {
		t.Fatalf("Content-Language = %q, want %q", got, localeChinese)
	}
	body := response.Body.String()
	for _, want := range []string{
		`<html lang="zh-CN">`,
		`href="/repositories?lang=en&amp;q=acme&#43;radar&amp;topic=ai-agent"`,
		`href="/repositories?lang=zh-CN&amp;q=acme&#43;radar&amp;topic=ai-agent"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %#v, want one locale cookie", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != localeCookieName || cookie.Value != localeChinese || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("locale cookie = %#v", cookie)
	}

	requestWithCookie := httptest.NewRequest(http.MethodGet, "/runs", nil)
	requestWithCookie.AddCookie(cookie)
	cookieResponse := httptest.NewRecorder()
	handler.ServeHTTP(cookieResponse, requestWithCookie)
	if !strings.Contains(cookieResponse.Body.String(), "最近任务运行") {
		t.Fatalf("locale cookie was not honored: %s", cookieResponse.Body.String())
	}
}

func TestUnsupportedLocaleFallsBackToEnglish(t *testing.T) {
	handler := newTestHandlerWithLocale(t, populatedFake(), "fr-FR")
	response := request(t, handler, "/?lang=fr-FR")
	if !strings.Contains(response.Body.String(), `<html lang="en">`) || !strings.Contains(response.Body.String(), "Monitoring summary") {
		t.Fatalf("unsupported locale did not fall back to English: %s", response.Body.String())
	}
	if values := response.Header().Values("Set-Cookie"); len(values) != 0 {
		t.Fatalf("unsupported locale set a cookie: %v", values)
	}
}

func TestTranslationCatalogsStayInSync(t *testing.T) {
	english := messageCatalog[localeEnglish]
	chinese := messageCatalog[localeChinese]
	for key, value := range english {
		if strings.TrimSpace(value) == "" {
			t.Errorf("English translation %q is empty", key)
		}
		if _, ok := chinese[key]; !ok {
			t.Errorf("Chinese catalog is missing %q", key)
		}
	}
	for key, value := range chinese {
		if strings.TrimSpace(value) == "" {
			t.Errorf("Chinese translation %q is empty", key)
		}
		if _, ok := english[key]; !ok {
			t.Errorf("English catalog is missing %q", key)
		}
	}
}

func TestEmptyDatabaseRendersInstructionalStates(t *testing.T) {
	handler := newTestHandler(t, &fakeQueryer{})
	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "No comparable growth yet"},
		{path: "/repositories", want: "No repositories match"},
		{path: "/topics", want: "No topics configured"},
		{path: "/discoveries", want: "No discoveries recorded"},
		{path: "/runs", want: "No job runs recorded"},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := request(t, handler, test.path)
			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", response.Code)
			}
			if !strings.Contains(response.Body.String(), test.want) {
				t.Fatalf("body does not contain %q", test.want)
			}
		})
	}
}

func TestRepositoryFiltersAndPaginationArePreserved(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Total = 120
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/repositories?q=acme+radar&topic=ai-agent&source=manual&status=active&offset=50")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	got := queryer.lastRepositoryQuery
	if got.Search != "acme radar" || got.TopicSlug != "ai-agent" || got.Source != "manual" || got.MonitoringStatus != "active" {
		t.Fatalf("unexpected filter: %#v", got)
	}
	if got.Limit != repositoryPageSize || got.Offset != 50 {
		t.Fatalf("unexpected pagination query: %#v", got)
	}
	body := response.Body.String()
	for _, value := range []string{"offset=100", "q=acme&#43;radar", "topic=ai-agent", "Showing 51 to 100 of 120"} {
		if !strings.Contains(body, value) {
			t.Errorf("body does not contain %q", value)
		}
	}
}

func TestTopicLeaderToggleIsPassedToQuery(t *testing.T) {
	queryer := populatedFake()
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/topics/ai-agent?exclude_leader=true")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if queryer.lastTopicSlug != "ai-agent" || !queryer.lastExcludeLeader {
		t.Fatalf("topic query = %q, exclude = %v", queryer.lastTopicSlug, queryer.lastExcludeLeader)
	}
}

func TestInvalidAndMissingResourcesReturnHTML404(t *testing.T) {
	queryer := populatedFake()
	handler := newTestHandler(t, queryer)
	for _, path := range []string{"/repositories/not-a-number", "/repositories/0", "/topics/Invalid_Slug", "/not-here"} {
		response := request(t, handler, path)
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status = %d, want 404", path, response.Code)
		}
		if !strings.Contains(response.Body.String(), "Page not found") {
			t.Errorf("%s did not render HTML 404", path)
		}
	}

	queryer.repositoryErr = ErrNotFound
	response := request(t, handler, "/repositories/404")
	if response.Code != http.StatusNotFound {
		t.Fatalf("missing repository status = %d, want 404", response.Code)
	}
}

func TestTemplatesEscapeContentAndRejectUnsafeLinks(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Items[0].Description = `<script>alert("owned")</script><b>bold</b>`
	queryer.repositories.Items[0].HTMLURL = "javascript:alert(1)"
	queryer.repository.Repository = queryer.repositories.Items[0]
	handler := newTestHandler(t, queryer)

	response := request(t, handler, "/repositories")
	body := response.Body.String()
	if strings.Contains(body, `<script>alert("owned")</script>`) || strings.Contains(body, "javascript:alert") {
		t.Fatalf("unsafe content was rendered: %s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;alert") || !strings.Contains(body, "&lt;b&gt;bold&lt;/b&gt;") {
		t.Fatalf("malicious description was not escaped: %s", body)
	}

	queryer.repository.Repository.HTMLURL = "https://example.com/acme/radar"
	response = request(t, handler, "/repositories/101")
	if strings.Contains(response.Body.String(), "https://example.com/acme/radar") {
		t.Fatal("non-GitHub external URL was rendered")
	}
}

func TestWarningsAndFailedObservationsStayVisible(t *testing.T) {
	queryer := populatedFake()
	queryer.dashboard.Warnings = []string{`Search profile <partial> was incomplete`}
	queryer.repository.History[1].Stars = nil
	queryer.repository.History[1].FetchStatus = "failed"
	queryer.repository.FailedDates = []SnapshotPoint{queryer.repository.History[1]}
	handler := newTestHandler(t, queryer)

	response := request(t, handler, "/")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Search profile &lt;partial&gt; was incomplete") {
		t.Fatalf("partial warning not rendered safely: %s", response.Body.String())
	}
	response = request(t, handler, "/repositories/101")
	body := response.Body.String()
	if !strings.Contains(body, "Failed observations") || !strings.Contains(body, "N/A") {
		t.Fatalf("failed observation was not explicit: %s", body)
	}
}

func TestChartPreservesMissingObservationAsGap(t *testing.T) {
	queryer := populatedFake()
	queryer.repository.History = []SnapshotPoint{
		{Date: mustDate("2026-08-27"), Stars: int64Pointer(100), FetchStatus: "success"},
		{Date: mustDate("2026-08-28"), Stars: nil, FetchStatus: "failed"},
		{Date: mustDate("2026-08-29"), Stars: int64Pointer(125), FetchStatus: "success"},
	}
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/repositories/101")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	if count := strings.Count(body, `class="chart-line"`); count != 2 {
		t.Fatalf("star chart line segment count = %d, want 2; body: %s", count, body)
	}
	if strings.Contains(body, ">0<") {
		t.Fatal("missing observation was rendered as a zero value")
	}
}

func TestDashboardGrowthChartDistinguishesNegativeZeroAndMissing(t *testing.T) {
	queryer := populatedFake()
	queryer.dashboard.GrowthHistory = []GrowthPoint{
		{Date: mustDate("2026-08-27"), Delta: int64Pointer(120), ComparableRepositoryCount: 100},
		{Date: mustDate("2026-08-28"), Delta: int64Pointer(0), ComparableRepositoryCount: 104},
		{Date: mustDate("2026-08-29"), Delta: nil},
		{Date: mustDate("2026-08-30"), Delta: int64Pointer(-35), ComparableRepositoryCount: 103, GapSpanningRepositoryCount: 2},
	}
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	body := response.Body.String()
	for _, want := range []string{
		`class="growth-bar-positive"`,
		`class="growth-bar-negative"`,
		`class="growth-bar-zero"`,
		`class="growth-bar-missing"`,
		"View exact daily values",
		">-35<",
		">0<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, `style="`) {
		t.Fatal("chart emitted an inline style, which violates the CSP")
	}
}

func TestDashboardGrowthChartNeedsAComparablePoint(t *testing.T) {
	queryer := populatedFake()
	queryer.dashboard.GrowthHistory = []GrowthPoint{{Date: mustDate("2026-08-30")}}
	body := request(t, newTestHandler(t, queryer), "/").Body.String()
	if strings.Contains(body, `class="growth-chart"`) {
		t.Fatal("all-missing growth history rendered a chart")
	}
	if !strings.Contains(body, "No comparable daily history yet") {
		t.Fatalf("all-missing history did not render the empty state: %s", body)
	}
}

func TestTopicRankingUsesLeafTopicsPeriodAndEightItemLimit(t *testing.T) {
	parent := TopicMetric{Slug: "agent", Name: "Agent", Delta30D: int64Pointer(99_999)}
	items := []TopicMetric{parent}
	for index := 0; index < 10; index++ {
		value := int64(10_000 - index)
		items = append(items, TopicMetric{
			Slug:            fmt.Sprintf("leaf-%d", index),
			Name:            fmt.Sprintf("Leaf %d", index),
			ParentSlug:      "agent",
			RepositoryCount: index + 1,
			CurrentStars:    int64Pointer(int64(1_000 + index)),
			Delta30D:        &value,
		})
	}
	ranking := makeTopicRankChart(items, "30d", newLocalizer(localeEnglish))
	if !ranking.HasData || ranking.Period != "30d" || len(ranking.Items) != 8 {
		t.Fatalf("ranking = %#v", ranking)
	}
	for _, item := range ranking.Items {
		if item.Slug == parent.Slug {
			t.Fatal("parent topic appeared in the leaf-topic ranking")
		}
	}
	if ranking.Items[0].Slug != "leaf-0" || ranking.Items[7].Slug != "leaf-7" {
		t.Fatalf("unexpected ranking bounds: first=%q last=%q", ranking.Items[0].Slug, ranking.Items[7].Slug)
	}

	queryer := populatedFake()
	body := request(t, newTestHandler(t, queryer), "/topics?period=30d&lang=zh-CN").Body.String()
	for _, want := range []string{
		`href="/topics?lang=zh-CN&amp;period=1d"`,
		`aria-current="page">近 30 天`,
		"叶子主题增长排行",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
}

func TestDiscoveryMixUsesStableSourceColorsAndKeepsZeroCounts(t *testing.T) {
	sources := []DiscoverySource{
		{Source: "manual", RepositoryCount: 0},
		{Source: "github_search", RepositoryCount: 75},
		{Source: "ossinsight", RepositoryCount: 25},
		{Source: "legacy", RepositoryCount: 0},
	}
	first := makeDiscoveryMixChart(sources, newLocalizer(localeEnglish))
	second := makeDiscoveryMixChart([]DiscoverySource{sources[2], sources[0], sources[3], sources[1]}, newLocalizer(localeEnglish))
	if !first.HasData || len(first.Segments) != 4 || len(second.Segments) != 4 {
		t.Fatalf("unexpected source charts: first=%#v second=%#v", first, second)
	}
	colors := func(chart discoveryMixChart) map[string]string {
		result := make(map[string]string)
		for _, segment := range chart.Segments {
			result[segment.Source] = segment.ColorClass
		}
		return result
	}
	for source, color := range colors(first) {
		if colors(second)[source] != color {
			t.Errorf("source %q color changed from %q to %q", source, color, colors(second)[source])
		}
	}

	queryer := populatedFake()
	queryer.discoveries.FirstSeenSources = sources
	body := request(t, newTestHandler(t, queryer), "/discoveries").Body.String()
	for _, want := range []string{"75.0%", "25.0%", "0 · 0.0%", "source-fill-1", "source-fill-2", "source-fill-3", "source-fill-4"} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "source-strip") {
		t.Fatal("legacy source strip is still rendered")
	}
}

func TestFatalQueryErrorIsGeneric(t *testing.T) {
	queryer := populatedFake()
	queryer.dashboardErr = errors.New("database failed with secret-token-value")
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/")
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", response.Code)
	}
	body := response.Body.String()
	if strings.Contains(body, "secret-token-value") {
		t.Fatal("internal error detail leaked to browser")
	}
	if !strings.Contains(body, "Data unavailable") {
		t.Fatal("generic error state was not rendered")
	}
}

func TestReadinessFailureAndSecurityHeaders(t *testing.T) {
	queryer := populatedFake()
	queryer.readyErr = errors.New("database unavailable")
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/readyz")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", response.Code)
	}

	response = request(t, handler, "/")
	for header, want := range map[string]string{
		"Content-Security-Policy": "default-src 'self'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
	} {
		if !strings.Contains(response.Header().Get(header), want) {
			t.Errorf("%s = %q, want it to contain %q", header, response.Header().Get(header), want)
		}
	}
}

func TestSearchInputIsBounded(t *testing.T) {
	queryer := populatedFake()
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/repositories?q="+strings.Repeat("a", 150))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if got := len([]rune(queryer.lastRepositoryQuery.Search)); got != 120 {
		t.Fatalf("search length = %d, want 120", got)
	}
}

func TestNewRejectsNilQueryer(t *testing.T) {
	if _, err := New(nil, Options{}); err == nil {
		t.Fatal("New(nil) returned no error")
	}
}

func populatedFake() *fakeQueryer {
	started := mustTime("2026-08-30T01:15:00Z")
	finished := started.Add(38 * time.Second)
	validFrom := mustDate("2026-08-27")
	lastRun := finished
	lastSnapshot := mustDate("2026-08-30")
	coverage := 99.2
	concentration := 42.4
	searchRemaining := 27
	coreRemaining := 4870
	repository := RepositoryMetric{
		ID:               101,
		FullName:         "acme/radar",
		HTMLURL:          "https://github.com/acme/radar",
		Description:      "A repository trend monitor.",
		PrimaryLanguage:  "Go",
		CurrentStars:     int64Pointer(12_450),
		Delta1D:          int64Pointer(85),
		Delta7D:          int64Pointer(430),
		Delta30D:         int64Pointer(1_220),
		Topics:           []TopicRef{{Slug: "ai-agent", Name: "AI Agent"}},
		FirstSeenSource:  "github_search",
		FirstSeenProfile: "topic-popular",
		FirstSeenAt:      mustTime("2026-08-27T02:00:00Z"),
		MonitoringStatus: "active",
		GitHubStatus:     "active",
	}
	run := JobRun{
		RunID:               "run-20260830-011500",
		JobType:             "run-daily",
		StartedAt:           started,
		FinishedAt:          &finished,
		Status:              "partial",
		TargetCount:         1_200,
		SuccessCount:        1_190,
		FailureCount:        10,
		SkippedCount:        0,
		FailureRepositories: []string{"acme/missing"},
		ErrorSummary:        "Ten repositories were unreachable.",
		SearchRateRemaining: &searchRemaining,
		CoreRateRemaining:   &coreRemaining,
		SearchSplitCount:    3,
	}
	coverageData := SnapshotCoverage{Date: lastSnapshot, Target: 1_200, Successful: 1_190, Failed: 10, Percent: &coverage}
	return &fakeQueryer{
		dashboard: DashboardSummary{
			RepositoryTotal:       1_250,
			ActiveRepositoryTotal: 1_200,
			TopicTotal:            8,
			Coverage:              coverageData,
			NewStars1D:            int64Pointer(2_450),
			NewStars7D:            int64Pointer(14_200),
			NewStars30D:           int64Pointer(52_300),
			GrowthHistory: []GrowthPoint{
				{Date: mustDate("2026-08-27"), Delta: int64Pointer(1_900), ComparableRepositoryCount: 1_170},
				{Date: mustDate("2026-08-28"), Delta: int64Pointer(2_200), ComparableRepositoryCount: 1_180},
				{Date: mustDate("2026-08-29"), Delta: int64Pointer(0), ComparableRepositoryCount: 1_185},
				{Date: mustDate("2026-08-30"), Delta: int64Pointer(2_450), ComparableRepositoryCount: 1_190},
			},
			FastestRepositories: []RepositoryMetric{repository},
			RecentRuns:          []JobRun{run},
		},
		repositories: RepositoryPage{
			Items:              []RepositoryMetric{repository},
			Total:              1,
			Topics:             []TopicRef{{Slug: "ai-agent", Name: "AI Agent"}},
			Sources:            []string{"ossinsight", "github_search", "legacy", "manual"},
			MonitoringStatuses: []string{"active", "paused", "stopped"},
		},
		repository: RepositoryDetail{
			Repository: repository,
			History: []SnapshotPoint{
				{Date: mustDate("2026-08-27"), Stars: int64Pointer(12_000), FetchStatus: "success", OSSRank: int64Pointer(9)},
				{Date: mustDate("2026-08-28"), Stars: int64Pointer(12_100), FetchStatus: "success", OSSRank: int64Pointer(7)},
				{Date: mustDate("2026-08-29"), Stars: int64Pointer(12_365), FetchStatus: "success", OSSRank: nil},
				{Date: mustDate("2026-08-30"), Stars: int64Pointer(12_450), FetchStatus: "success", OSSRank: int64Pointer(4)},
			},
			PreviousNames: []string{"acme/star-radar"},
			ValidFrom:     &validFrom,
		},
		topics: TopicPage{Items: []TopicMetric{{
			ID:              1,
			Slug:            "ai-agent",
			Name:            "AI Agent",
			Description:     "Agent frameworks and runtimes.",
			RepositoryCount: 120,
			CurrentStars:    int64Pointer(3_200_000),
			Delta1D:         int64Pointer(5_200),
			Delta7D:         int64Pointer(31_400),
			Delta30D:        int64Pointer(120_000),
		}}},
		topic: TopicDetail{
			Topic: TopicMetric{
				ID:              1,
				Slug:            "ai-agent",
				Name:            "AI Agent",
				Description:     "Agent frameworks and runtimes.",
				RepositoryCount: 120,
				CurrentStars:    int64Pointer(3_200_000),
				Delta1D:         int64Pointer(5_200),
				Delta7D:         int64Pointer(31_400),
				Delta30D:        int64Pointer(120_000),
			},
			Repositories:           []RepositoryMetric{repository},
			History:                []TrendPoint{{Date: mustDate("2026-08-29"), Stars: int64Pointer(3_194_800)}, {Date: mustDate("2026-08-30"), Stars: int64Pointer(3_200_000)}},
			ConcentrationPercent:   &concentration,
			ExcludedLeaderFullName: "acme/radar",
		},
		discoveries: DiscoverySummary{
			Sources: []DiscoverySource{
				{Source: "ossinsight", RepositoryCount: 340, LastRunAt: &lastRun},
				{Source: "github_search", RepositoryCount: 650, LastRunAt: &lastRun},
				{Source: "legacy", RepositoryCount: 250, LastRunAt: &lastRun},
				{Source: "manual", RepositoryCount: 10, LastRunAt: &lastRun},
			},
			FirstSeenSources: []DiscoverySource{
				{Source: "ossinsight", RepositoryCount: 340},
				{Source: "github_search", RepositoryCount: 650},
				{Source: "legacy", RepositoryCount: 250},
				{Source: "manual", RepositoryCount: 10},
			},
			Profiles: []DiscoveryProfile{{Name: "topic-popular", NewRepositories: 12, CandidateCount: 420, IncompleteResults: true, QuerySplitCount: 3, LastRunAt: &lastRun}},
		},
		runs: RunsPage{
			Items:                      []JobRun{run},
			Total:                      1,
			LastSuccessfulSnapshotDate: &lastSnapshot,
			CurrentCoverage:            coverageData,
		},
	}
}

func newTestHandler(t *testing.T, queryer Queryer) http.Handler {
	return newTestHandlerWithLocale(t, queryer, "")
}

func newTestHandlerWithLocale(t *testing.T, queryer Queryer, locale string) http.Handler {
	t.Helper()
	handler, err := New(queryer, Options{
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
		Now:      func() time.Time { return mustTime("2026-08-30T03:00:00Z") },
		Location: time.UTC,
		Locale:   locale,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return handler
}

func request(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func int64Pointer(value int64) *int64 {
	return &value
}

func mustDate(value string) time.Time {
	result, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return result
}

func mustTime(value string) time.Time {
	result, err := time.Parse(time.RFC3339, value)
	if err != nil {
		panic(err)
	}
	return result
}

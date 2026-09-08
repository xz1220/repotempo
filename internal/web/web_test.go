package web

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeQueryer struct {
	radar               RadarOverview
	radarErr            error
	lastRadarQuery      RepositoryQuery
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
	lastRepositoryAsOf  time.Time
	lastTopicSlug       string
	lastExcludeLeader   bool
	lastRunLimit        int
	lastRunOffset       int
}

func (f *fakeQueryer) RadarOverview(_ context.Context, query RepositoryQuery) (RadarOverview, error) {
	f.lastRadarQuery = query
	return f.radar, f.radarErr
}

func (f *fakeQueryer) DashboardSummary(context.Context, time.Time) (DashboardSummary, error) {
	return f.dashboard, f.dashboardErr
}

func (f *fakeQueryer) ListRepositoryMetrics(_ context.Context, query RepositoryQuery) (RepositoryPage, error) {
	f.lastRepositoryQuery = query
	return f.repositories, f.repositoriesErr
}

func (f *fakeQueryer) ListRepositoryTrends(_ context.Context, query RepositoryQuery) (RepositoryPage, error) {
	f.lastRepositoryQuery = query
	return f.repositories, f.repositoriesErr
}

func (f *fakeQueryer) GetRepositoryDetail(_ context.Context, id int64, asOf time.Time) (RepositoryDetail, error) {
	f.lastRepositoryID = id
	f.lastRepositoryAsOf = asOf
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
		{path: "/", wantStatus: http.StatusOK, wantContent: "Follow what happens next."},
		{path: "/repositories", wantStatus: http.StatusOK, wantContent: "Project library"},
		{path: "/discoveries", wantStatus: http.StatusFound, wantContent: "Found"},
		{path: "/repositories/101", wantStatus: http.StatusOK, wantContent: "Valid history starts"},
		{path: "/topics", wantStatus: http.StatusOK, wantContent: "General agents"},
		{path: "/topics/ai-agent", wantStatus: http.StatusOK, wantContent: "Top repository share"},
		{path: "/runs", wantStatus: http.StatusOK, wantContent: "Recent job runs"},
		{path: "/static/tokens.css", wantStatus: http.StatusOK, wantContent: "--color-accent:"},
		{path: "/static/app.css", wantStatus: http.StatusOK, wantContent: ".app-sidebar"},
		{path: "/static/app.js", wantStatus: http.StatusOK, wantContent: "document.documentElement"},
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

func TestStaticAssetsRevalidateWithETag(t *testing.T) {
	handler := newTestHandler(t, populatedFake())
	first := request(t, handler, "/static/app.css")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("static asset response is missing an ETag")
	}
	if cacheControl := first.Header().Get("Cache-Control"); cacheControl != "public, max-age=0, must-revalidate" {
		t.Fatalf("Cache-Control = %q", cacheControl)
	}

	requestWithETag := httptest.NewRequest(http.MethodGet, "/static/app.css", nil)
	requestWithETag.Header.Set("If-None-Match", etag)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, requestWithETag)
	if response.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotModified)
	}
}

func TestChineseLocaleRendersAllProductRoutes(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Warnings = []string{"Current snapshot coverage is temporarily unavailable."}
	handler := newTestHandlerWithLocale(t, queryer, localeChinese)

	tests := []struct {
		path string
		want string
	}{
		{path: "/", want: "开源项目，持续关注"},
		{path: "/repositories", want: "项目库"},
		{path: "/repositories?new=1", want: "仅看当天新入库"},
		{path: "/repositories/101", want: "有效历史起始日"},
		{path: "/topics", want: "主题指标"},
		{path: "/topics/ai-agent", want: "头部项目占比"},
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
	for _, want := range []string{"涨势最快 Top 10", "势头回落", "数据日期 2026-08-30"} {
		if !strings.Contains(home, want) {
			t.Errorf("Chinese project monitor does not contain %q", want)
		}
	}
	library := request(t, handler, "/repositories").Body.String()
	if !strings.Contains(library, "当前快照覆盖率暂不可用。") {
		t.Error("Chinese library warning is missing")
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

func TestWorkspaceNavigationAndDetailOrientationRender(t *testing.T) {
	handler := newTestHandler(t, populatedFake())
	home := request(t, handler, "/").Body.String()
	for _, want := range []string{
		`href="/static/tokens.css"`,
		`class="app-sidebar"`,
		`class="primary-nav"`,
		`>GitHub projects<`,
		`>Trends<`,
		`>History<`,
		`href="/runs"`,
	} {
		if !strings.Contains(home, want) {
			t.Errorf("home does not contain %q: %s", want, home)
		}
	}
	navParts := strings.SplitN(home, `<nav class="primary-nav"`, 2)
	if len(navParts) != 2 {
		t.Fatal("primary navigation is missing")
	}
	navigation := strings.SplitN(navParts[1], "</nav>", 2)[0]
	if count := strings.Count(navigation, `class="nav-link `); count != 2 {
		t.Errorf("primary navigation link count = %d, want 2", count)
	}
	for _, path := range []string{"/runs", "/discoveries", "/topics", "/watch/new", "/repositories?focus=1"} {
		if strings.Contains(navigation, `href="`+path+`"`) {
			t.Errorf("primary navigation unexpectedly contains %s", path)
		}
	}
	if strings.Contains(home, `href="/watch/new"`) || strings.Contains(home, `href="/repositories?focus=1"`) {
		t.Error("global add/watchlist shortcuts should not remain on the dashboard")
	}

	projects := request(t, handler, "/repositories").Body.String()
	for _, want := range []string{
		`class="project-card"`,
		`class="repository-feed"`,
		`href="/repositories/101?date=2026-08-30&amp;lang=en"`,
		`action="/repositories"`,
	} {
		if !strings.Contains(projects, want) {
			t.Errorf("projects does not contain %q: %s", want, projects)
		}
	}
	if count := strings.Count(projects, `href="/watch/new"`); count != 1 {
		t.Errorf("project library add entry count = %d, want exactly one", count)
	}

	repository := request(t, handler, "/repositories/101").Body.String()
	if !strings.Contains(repository, `class="breadcrumb"`) ||
		!strings.Contains(repository, `aria-label="Breadcrumb"`) ||
		!strings.Contains(repository, `href="/repositories"`) ||
		!strings.Contains(repository, `class="entity-title-repository" translate="no"`) {
		t.Fatalf("repository breadcrumb is missing: %s", repository)
	}

	topic := request(t, handler, "/topics/ai-agent").Body.String()
	if !strings.Contains(topic, `class="breadcrumb"`) ||
		!strings.Contains(topic, `href="/topics"`) ||
		!strings.Contains(topic, `translate="no">General agents</h1>`) {
		t.Fatalf("topic breadcrumb is missing: %s", topic)
	}
	for _, path := range []string{"/topics", "/topics/ai-agent"} {
		body := request(t, handler, path).Body.String()
		if !strings.Contains(body, `class="nav-link is-active" href="/repositories" aria-current="page"`) {
			t.Errorf("%s should keep Projects active", path)
		}
	}
}

func TestRepoTempoBrandAndSourceAttributionRenderInBothLanguages(t *testing.T) {
	for _, locale := range []string{localeEnglish, localeChinese} {
		handler := newTestHandlerWithLocale(t, populatedFake(), locale)
		subtitle := "independent GitHub trends tracker"
		if locale == localeChinese {
			subtitle = "开源趋势观察"
		}
		for _, path := range []string{"/", "/repositories", "/repositories/101", "/watch/new"} {
			body := html.UnescapeString(request(t, handler, path).Body.String())
			for _, want := range []string{" · RepoTempo</title>", "RepoTempo · MIT", subtitle, `href="https://github.com/xz1220/repotempo"`, "GitHub Trending", "Search"} {
				if !strings.Contains(body, want) {
					t.Errorf("%s (%s) missing new branding/source attribution %q", path, locale, want)
				}
			}
			if strings.Contains(body, "GitHub Radar") || strings.Contains(body, "https://github.com/xz1220/github-radar") {
				t.Errorf("%s (%s) still uses the old public brand", path, locale)
			}
		}
	}
}

func TestUnsupportedLocaleFallsBackToEnglish(t *testing.T) {
	handler := newTestHandlerWithLocale(t, populatedFake(), "fr-FR")
	response := request(t, handler, "/?lang=fr-FR")
	if !strings.Contains(response.Body.String(), `<html lang="en">`) || !strings.Contains(response.Body.String(), "Follow what happens next.") {
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
		{path: "/", want: "No comparable positive growth in this period"},
		{path: "/repositories?new=1", want: "No matching additions on 2026-08-30"},
		{path: "/repositories", want: "No matching additions on 2026-08-30"},
		{path: "/repositories?view=all", want: "No repositories match"},
		{path: "/topics", want: "No topics configured"},
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

func TestRepositoryTrendControlsQueryAndPreserveCursor(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Total = 120
	queryer.repositories.HasMore = true
	queryer.repositories.NextCursor = "2t"
	queryer.repositories.Items[0].IsNew = true
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/repositories?q=acme+radar&topic=ai-agent&date=2026-08-29&period=30d&sort=rank_change&new=1&focus=1&source=manual&status=active&cursor=2s&lang=en")

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	got := queryer.lastRepositoryQuery
	if got.Search != "acme radar" || got.TopicSlug != "ai-agent" || got.Source != "manual" || got.MonitoringStatus != "active" || got.Sort != "rank_change" || !got.OnlyNew || !got.OnlyFocus {
		t.Fatalf("unexpected filter: %#v", got)
	}
	if got.WindowDays != 30 || got.AsOf.Format("2006-01-02") != "2026-08-29" {
		t.Fatalf("unexpected comparison window: %#v", got)
	}
	if got.Limit != repositoryPageSize || got.AfterID == nil || *got.AfterID != 100 {
		t.Fatalf("unexpected pagination query: %#v", got)
	}
	body := html.UnescapeString(response.Body.String())
	for _, value := range []string{
		`href="/repositories?cursor=2t&date=2026-08-30&focus=1&lang=en&new=1&period=30d&q=acme+radar&sort=rank_change&source=manual&status=active&topic=ai-agent"`,
		`value="30d" selected`,
		`value="rank_change" selected`,
		`name="focus" value="1"`,
		`name="new" value="1"`,
		`120 repositories`,
		`class="new-badge">New`,
		`class="project-card-rank"`,
		`<span>7</span><span aria-hidden="true">→</span><strong>4</strong>`,
		`+3 places`,
		`+430`,
	} {
		if !strings.Contains(body, value) {
			t.Errorf("body does not contain %q", value)
		}
	}
}

func TestLegacyDiscoveriesRedirectToNewProjectsWithoutLosingFilters(t *testing.T) {
	for _, test := range []struct{ path, want string }{
		{"/discoveries", "/repositories?new=1&period=1d&sort=stars"},
		{"/discoveries?lang=zh-CN&q=radar&date=2026-08-30&focus=1&topic=research-agents&cursor=abc&new=0", "/repositories?date=2026-08-30&focus=1&lang=zh-CN&new=1&period=1d&q=radar&sort=stars&topic=research-agents"},
		{"/discoveries?period=30d&sort=growth_rate&date=2026-08-29&cursor=abc", "/repositories?date=2026-08-29&new=1&period=30d&sort=growth_rate"},
	} {
		t.Run(test.path, func(t *testing.T) {
			queryer := populatedFake()
			handler := newTestHandler(t, queryer)
			response := request(t, handler, test.path)
			if response.Code != http.StatusFound || response.Header().Get("Location") != test.want {
				t.Fatalf("legacy redirect = %d %q, want 302 %q", response.Code, response.Header().Get("Location"), test.want)
			}
			if queryer.lastRepositoryQuery.Limit != 0 {
				t.Fatal("legacy route queried the library before redirecting")
			}
			response = request(t, handler, response.Header().Get("Location"))
			if response.Code != http.StatusOK || !queryer.lastRepositoryQuery.OnlyNew || queryer.lastRepositoryQuery.AfterID != nil {
				t.Fatalf("redirected library did not apply new-project filter: %+v", queryer.lastRepositoryQuery)
			}
		})
	}
}

func TestStaleRepositoryCursorRedirectsToFirstPage(t *testing.T) {
	queryer := populatedFake()
	queryer.repositoriesErr = ErrInvalid
	response := request(t, newTestHandler(t, queryer), "/repositories?topic=skills&period=7d&cursor=stale&lang=zh-CN")
	if response.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", response.Code)
	}
	location, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if location.Query().Has("cursor") || location.Query().Get("topic") != "skills" || location.Query().Get("period") != "7d" || location.Query().Get("lang") != localeChinese {
		t.Fatalf("cursor recovery location = %q", location.String())
	}
}

func TestInvalidRepositoryDateReturnsBadRequest(t *testing.T) {
	queryer := populatedFake()
	response := request(t, newTestHandlerWithLocale(t, queryer, localeChinese), "/?date=2026-02-30")
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if !strings.Contains(response.Body.String(), "观测日期无效") || !strings.Contains(response.Body.String(), "YYYY-MM-DD") {
		t.Fatalf("invalid date response = %s", response.Body.String())
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
	queryer.repositories.Warnings = []string{`Search profile <partial> was incomplete`}
	queryer.repository.History[1].Stars = nil
	queryer.repository.History[1].FetchStatus = "failed"
	queryer.repository.FailedDates = []SnapshotPoint{queryer.repository.History[1]}
	handler := newTestHandler(t, queryer)

	response := request(t, handler, "/repositories")
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

func TestRepositoryDetailRendersStoredProjectAnalysis(t *testing.T) {
	queryer := populatedFake()
	queryer.repository.Analysis = &RepositoryAnalysis{
		SummaryZH:      "这个项目持续追踪 GitHub 项目的 Star 变化。",
		KeyPoints:      []string{"记录每日 Star 快照", "区分缺失观测与真实零增长"},
		UseCases:       []string{"观察开源项目的增长势头"},
		TechnicalNotes: "分析结果离线生成，Web 服务只读展示。",
		Source:         "codex",
		Model:          "gpt-5.4",
		Revision:       2,
		AnalyzedAt:     mustDate("2026-08-30"),
	}
	body := request(t, newTestHandler(t, queryer), "/repositories/101").Body.String()
	for _, want := range []string{
		"About this project",
		"这个项目持续追踪 GitHub 项目的 Star 变化。",
		"记录每日 Star 快照",
		"观察开源项目的增长势头",
		"分析结果离线生成，Web 服务只读展示。",
		"codex analysis · revision 2 · 2026-08-30",
		"gpt-5.4",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("stored analysis does not contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Codex interpretation has not been generated yet") {
		t.Fatal("stored analysis rendered the pending fallback")
	}
}

func TestRepositoryDetailFallbackExplainsProjectAndPreservesManualReason(t *testing.T) {
	queryer := populatedFake()
	queryer.repository.Analysis = nil
	queryer.repository.Repository.ManualNote = "这是一条已经保存的人工项目说明。"
	body := request(t, newTestHandler(t, queryer), "/repositories/101").Body.String()
	for _, want := range []string{"About this project", "Repository description", "A repository trend monitor.", "Why follow: 这是一条已经保存的人工项目说明。", `href="https://github.com/acme/radar#readme"`} {
		if !strings.Contains(body, want) {
			t.Errorf("manual-note fallback does not contain %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Codex interpretation has not been generated yet") {
		t.Fatal("a missing optional interpretation should not obscure the project description")
	}

	queryer.repository.Repository.ManualNote = ""
	body = request(t, newTestHandler(t, queryer), "/repositories/101").Body.String()
	if !strings.Contains(body, "A repository trend monitor.") || strings.Contains(body, `class="watch-note"`) {
		t.Fatalf("GitHub description fallback is missing: %s", body)
	}
}

func TestDashboardGrowthChartModelDistinguishesNegativeZeroAndMissing(t *testing.T) {
	history := []GrowthPoint{
		{Date: mustDate("2026-08-27"), Delta: int64Pointer(120), ComparableRepositoryCount: 100},
		{Date: mustDate("2026-08-28"), Delta: int64Pointer(0), ComparableRepositoryCount: 104},
		{Date: mustDate("2026-08-29"), Delta: nil},
		{Date: mustDate("2026-08-30"), Delta: int64Pointer(-35), ComparableRepositoryCount: 103, GapSpanningRepositoryCount: 2},
	}
	chart := dashboardGrowthChart(history, newLocalizer(localeEnglish))
	if !chart.HasPoints || !chart.HasComparable || len(chart.Bars) != 4 {
		t.Fatalf("growth chart = %#v", chart)
	}
	wantClasses := []string{"growth-bar-positive", "growth-bar-zero", "growth-bar-missing", "growth-bar-negative"}
	for index, want := range wantClasses {
		if chart.Bars[index].Class != want {
			t.Errorf("bar %d class = %q, want %q", index, chart.Bars[index].Class, want)
		}
	}
	if !chart.Bars[2].Missing || !strings.Contains(chart.Bars[3].Tooltip, "-35") {
		t.Fatalf("missing/negative observations were not represented: %#v", chart.Bars)
	}
}

func TestDashboardGrowthChartModelNeedsAComparablePoint(t *testing.T) {
	chart := dashboardGrowthChart([]GrowthPoint{{Date: mustDate("2026-08-30")}}, newLocalizer(localeEnglish))
	if !chart.HasPoints || chart.HasComparable || len(chart.Bars) != 1 || !chart.Bars[0].Missing {
		t.Fatalf("all-missing growth chart = %#v", chart)
	}
}

func TestLineChartExactValuesKeepMissingObservations(t *testing.T) {
	value := int64(42)
	chart := makeChart([]chartInput{
		{Date: mustDate("2026-08-28"), Value: &value},
		{Date: mustDate("2026-08-29")},
	}, false, "History", "History with a gap")
	if len(chart.Points) != 1 || len(chart.Observations) != 2 {
		t.Fatalf("chart points=%d observations=%d", len(chart.Points), len(chart.Observations))
	}
	if !chart.Observations[1].Missing || chart.Observations[1].Label != "2026-08-29" {
		t.Fatalf("missing observation = %#v", chart.Observations[1])
	}
}

func TestAllMissingLineChartStillRendersExactValues(t *testing.T) {
	queryer := populatedFake()
	queryer.topic.History = []TrendPoint{{Date: mustDate("2026-08-30")}}
	body := request(t, newTestHandler(t, queryer), "/topics/ai-agent").Body.String()
	if strings.Contains(body, `class="line-chart"`) {
		t.Fatal("all-missing topic history rendered a line chart")
	}
	if !strings.Contains(body, "No valid points yet") ||
		!strings.Contains(body, "View exact values") ||
		!strings.Contains(body, ">Missing<") {
		t.Fatalf("all-missing topic history did not preserve exact values: %s", body)
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
		`class="category-catalog"`,
		`href="/repositories?topic=ai-agent"`,
		"通用 Agent",
		"查看分类的增长数据",
		"80/120 可比",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q: %s", want, body)
		}
	}
}

func TestTopicsExposeClassificationAndComparableCoverage(t *testing.T) {
	body := request(t, newTestHandlerWithLocale(t, populatedFake(), localeChinese), "/topics?period=7d").Body.String()
	for _, want := range []string{
		"主题分类覆盖",
		"监测范围内",
		">1,250<",
		"已分类",
		">850<",
		"未分类",
		">400<",
		"68.0%",
		"90/120 可比",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("topic coverage does not contain %q: %s", want, body)
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
	for _, segment := range first.Segments {
		if (segment.Source == "manual" || segment.Source == "legacy") && segment.Percent != "0.0%" {
			t.Errorf("zero-count source %q percent = %q", segment.Source, segment.Percent)
		}
	}
}

func TestFatalQueryErrorIsGeneric(t *testing.T) {
	queryer := populatedFake()
	queryer.repositoriesErr = errors.New("database failed with secret-token-value")
	queryer.radarErr = queryer.repositoriesErr
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
		BaselineStars:    int64Pointer(12_020),
		CurrentRank:      int64Pointer(4),
		BaselineRank:     int64Pointer(7),
		RankChange:       int64Pointer(3),
		StarDelta:        int64Pointer(430),
		GrowthRate:       float64Pointer(3.6),
		DailyVelocity:    float64Pointer(61.4),
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
		radar: populatedRadar(repository),
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
			Coverage: ComparisonCoverage{
				BaselineDate:    mustDate("2026-08-23"),
				AsOfDate:        mustDate("2026-08-30"),
				ScopeCount:      1_250,
				ObservedCount:   1_200,
				ComparableCount: 1_150,
				NewCount:        12,
			},
		},
		repository: RepositoryDetail{
			AsOf:       lastSnapshot,
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
		topics: TopicPage{AsOf: lastSnapshot, Classification: TopicClassificationCoverage{
			RepositoryCount: 1_250, ClassifiedCount: 850, UnclassifiedCount: 400, Percent: float64Pointer(68),
		}, Items: []TopicMetric{{
			ID:              1,
			Slug:            "ai-agent",
			Name:            "AI Agent",
			Description:     "Agent frameworks and runtimes.",
			RepositoryCount: 120,
			CurrentStars:    int64Pointer(3_200_000),
			Delta1D:         int64Pointer(5_200),
			Delta7D:         int64Pointer(31_400),
			Delta30D:        int64Pointer(120_000),
			Comparable1D:    100,
			Comparable7D:    90,
			Comparable30D:   80,
		}}},
		topic: TopicDetail{
			AsOf: lastSnapshot,
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
				Comparable1D:    100,
				Comparable7D:    90,
				Comparable30D:   80,
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

func float64Pointer(value float64) *float64 {
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

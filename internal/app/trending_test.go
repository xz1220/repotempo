package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

type trendingAppFixture struct {
	runtime  *Runtime
	settings Settings
	server   *httptest.Server
	clock    atomic.Int64
	mu       sync.Mutex
	calls    map[string]int
	order    []string
}

func newTrendingAppFixture(t *testing.T, mode string, search bool, handler func(http.ResponseWriter, *http.Request)) *trendingAppFixture {
	t.Helper()
	fixture := &trendingAppFixture{calls: map[string]int{}}
	fixture.clock.Store(time.Date(2026, 9, 7, 2, 0, 0, 0, time.UTC).UnixNano())
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Path
		if key == "/trending" {
			key += "?since=" + r.URL.Query().Get("since")
			if r.Header.Get("Authorization") != "" {
				t.Error("GitHub API token leaked to Trending HTML request")
			}
		}
		fixture.mu.Lock()
		fixture.calls[key]++
		fixture.order = append(fixture.order, key)
		fixture.mu.Unlock()
		handler(w, r)
	}))
	t.Cleanup(fixture.server.Close)
	directory := t.TempDir()
	fixture.settings = Settings{
		DatabasePath: filepath.Join(directory, "radar.db"), DiscoveryConfig: filepath.Join(directory, "discovery.yaml"),
		TopicsConfig:    filepath.Join("..", "..", "config", "topics.example.yaml"),
		ExportDirectory: filepath.Join(directory, "exports"), BackupDirectory: filepath.Join(directory, "backups"),
		ExportRetention: 30, BackupRetention: 30, GitHubToken: "fixture-token",
	}
	trendingConfig := ""
	if mode == "enabled" {
		trendingConfig = fmt.Sprintf("trending:\n  enabled: true\n  base_url: %s/\n  timeout: 2s\n  interval: 1s\n  periods: [daily, weekly, monthly]\n", fixture.server.URL)
	} else if mode == "disabled" {
		trendingConfig = "trending:\n  enabled: false\n"
	}
	profiles := "profiles: []\n"
	if search {
		profiles = `profiles:
  - name: test-search
    schedule: daily
    sort: stars
    order: desc
    partition: {by: none, max_depth: 0}
    queries:
      - topic: ai-agent
`
	}
	writeFixture(t, fixture.settings.DiscoveryConfig, fmt.Sprintf(`version: 1
github:
  api_base_url: %s/
  user_agent: github-radar-test
  timeout: 2s
  core_interval: 1ms
  search_interval: 1ms
  secondary_backoff: 1m
  server_backoff: 1ms
  max_retries: 0
ossinsight:
  enabled: false
%s%smanual:
  files: []
legacy:
  database_path: ""
`, fixture.server.URL, trendingConfig, profiles))
	var err error
	fixture.runtime, err = OpenRuntimeWithOptions(context.Background(), fixture.settings, fixture.options(false))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if fixture.runtime != nil {
			_ = fixture.runtime.Close()
		}
	})
	return fixture
}

func (fixture *trendingAppFixture) now() time.Time { return time.Unix(0, fixture.clock.Load()).UTC() }

func (fixture *trendingAppFixture) options(readOnly bool) RuntimeOptions {
	return RuntimeOptions{Now: fixture.now, HTTPClient: fixture.server.Client(), ReadOnly: readOnly,
		CheckRuntime: func(Settings) DoctorReport { return DoctorReport{Healthy: true} }}
}

func (fixture *trendingAppFixture) count(path string) int {
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	return fixture.calls[path]
}

func (fixture *trendingAppFixture) seed(t *testing.T, id int64, name string, source domain.DiscoverySource) {
	t.Helper()
	_, _, err := fixture.runtime.store.UpsertRepository(context.Background(), domain.RepositoryObservation{
		GitHubRepoID: id, FullName: name, Source: source, DiscoveredAt: fixture.now().AddDate(0, 0, -3),
		MonitoringStatus: domain.MonitoringActive, GitHubStatus: domain.GitHubActive,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func trendingTestRow(name, total, gain, period string) string {
	unit := map[string]string{"daily": "today", "weekly": "this week", "monthly": "this month"}[period]
	return fmt.Sprintf(`<article class="Box-row"><h2><a href="/%s">%s</a></h2><a href="/%s/stargazers">%s</a><span class="float-sm-right">%s stars %s</span></article>`, name, name, name, total, gain, unit)
}

func writeTrendingTestPage(w http.ResponseWriter, rows string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, "<!doctype html><html><head><title>Trending repositories</title></head><body><h1>Trending</h1>%s</body></html>", rows)
}

func trendingTestRepository(id int64, name string, stars int64) string {
	return fmt.Sprintf(`{"id":%d,"full_name":%q,"html_url":%q,"description":"An AI coding agent","stargazers_count":%d,"forks_count":1,"private":false,"archived":false,"fork":false,"topics":["coding-agent"]}`, id, name, "https://github.com/"+name, stars)
}

func writeTrendingTestRepository(w http.ResponseWriter, id int64, name string, stars int64) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, trendingTestRepository(id, name, stars))
}

func writeTrendingTestSearch(w http.ResponseWriter, repositories ...string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"total_count":%d,"incomplete_results":false,"items":[%s]}`, len(repositories), strings.Join(repositories, ","))
}

func hasTrendingSource(values []domain.DiscoverySource, source domain.DiscoverySource) bool {
	for _, value := range values {
		if value == source {
			return true
		}
	}
	return false
}

func TestTrendingDiscoveryDeduplicatesWindowsAndSearchAndKeepsPageCountsAsEvidence(t *testing.T) {
	fixture := newTrendingAppFixture(t, "enabled", true, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			name := "owner/shared"
			if r.URL.Query().Get("since") == "weekly" {
				name = "OWNER/SHARED"
			}
			writeTrendingTestPage(w, trendingTestRow(name, "9,999", "789", r.URL.Query().Get("since")))
		case "/repos/owner/shared":
			writeTrendingTestRepository(w, 101, "owner/shared", 2222)
		case "/search/repositories":
			writeTrendingTestSearch(w, trendingTestRepository(101, "owner/shared", 3333))
		case "/repositories/101":
			writeTrendingTestRepository(w, 101, "owner/shared", 4444)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	report, err := fixture.runtime.Discover(ctx, DiscoverOptions{Source: "all", IncludeManual: true})
	if err != nil || report.Partial() || report.Trending == nil || report.Trending.ResolvedCount != 1 || report.CreatedCount != 1 {
		t.Fatalf("discovery = %+v %v", report, err)
	}
	if fixture.count("/repos/owner/shared") != 1 || fixture.count("/repos/OWNER/SHARED") != 0 || fixture.count("/search/repositories") != 1 {
		t.Fatalf("duplicate resolution: %v", fixture.calls)
	}
	repositories, err := fixture.runtime.store.ListRepositories(ctx, domain.RepositoryFilter{})
	if err != nil || len(repositories) != 1 {
		t.Fatalf("registry = %+v %v", repositories, err)
	}
	repository := repositories[0]
	if repository.FirstSeenSource != domain.DiscoverySourceGitHubTrending || !hasTrendingSource(repository.DiscoverySources, domain.DiscoverySourceGitHubTrending) || !hasTrendingSource(repository.DiscoverySources, domain.DiscoverySourceGitHubSearch) {
		t.Errorf("provenance = %+v", repository)
	}
	if _, err := fixture.runtime.store.GetDailySnapshot(ctx, 101, domain.ShanghaiDate(fixture.now())); err != corestore.ErrNotFound {
		t.Fatalf("discovery wrote a daily snapshot: %v", err)
	}
	for _, window := range report.Trending.Windows {
		if window.Error != "" || len(window.Entries) != 1 || *window.Entries[0].TotalStars != 9999 || *window.Entries[0].StarsInPeriod != 789 || window.Entries[0].Rank != 1 {
			t.Fatalf("page evidence = %+v", window)
		}
	}
	snapshotReport, err := fixture.runtime.Snapshot(ctx, false)
	if err != nil || snapshotReport.SuccessCount != 1 {
		t.Fatalf("snapshot = %+v %v", snapshotReport, err)
	}
	stored, err := fixture.runtime.store.GetDailySnapshot(ctx, 101, domain.ShanghaiDate(fixture.now()))
	if err != nil || stored.StarCount == nil || *stored.StarCount != 4444 || stored.OSSWindowStars != nil || stored.OSSTodayRank != nil || stored.HTTPStatus == nil || *stored.HTTPStatus != 200 {
		t.Fatalf("API count or evidence mixed: %+v %v", stored, err)
	}
	runs, err := fixture.runtime.store.ListJobRuns(ctx, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, run := range runs {
		if run.JobType != "discover" {
			continue
		}
		var details DiscoverReport
		if err := json.Unmarshal(run.Details, &details); err != nil {
			t.Fatal(err)
		}
		found = run.Status == domain.JobSuccess && details.Trending != nil && len(details.Trending.Windows) == 3 && *details.Trending.Windows[0].Entries[0].TotalStars == 9999
	}
	if !found {
		t.Fatal("complete Trending evidence missing from stored discovery job")
	}
}

func TestTrendingSourceFilterIncludesPreviouslySearchDiscoveredProjects(t *testing.T) {
	fixture := newTrendingAppFixture(t, "enabled", false, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			writeTrendingTestPage(w, trendingTestRow("owner/existing", "900", "9", r.URL.Query().Get("since")))
		case "/repos/owner/existing":
			writeTrendingTestRepository(w, 202, "owner/existing", 1000)
		default:
			http.NotFound(w, r)
		}
	})
	fixture.seed(t, 202, "owner/existing", domain.DiscoverySourceGitHubSearch)
	fixture.seed(t, 203, "owner/search-only", domain.DiscoverySourceGitHubSearch)
	report, err := fixture.runtime.Discover(context.Background(), DiscoverOptions{Source: "github-trending"})
	if err != nil || report.Partial() || report.CreatedCount != 0 || report.UpdatedCount != 1 {
		t.Fatalf("discovery = %+v %v", report, err)
	}
	stored, err := fixture.runtime.store.GetRepository(context.Background(), 202)
	if err != nil || stored.FirstSeenSource != domain.DiscoverySourceGitHubSearch || !hasTrendingSource(stored.DiscoverySources, domain.DiscoverySourceGitHubTrending) {
		t.Fatalf("first source changed or repeat source lost: %+v %v", stored, err)
	}
	page, err := fixture.runtime.store.ListRepositoryTrends(context.Background(), domain.RepositoryTrendQuery{AsOf: domain.ShanghaiDate(fixture.now()), WindowDays: 1, DiscoverySource: domain.DiscoverySourceGitHubTrending})
	if err != nil || len(page.Items) != 1 || page.Items[0].Repository.GitHubRepoID != 202 {
		t.Fatalf("Trending filter omitted later appearance: %+v %v", page, err)
	}
}

func TestTrendingDailyRetriesSkipCompletedBoardsAndKeepMonitoringAfterDelisting(t *testing.T) {
	var day atomic.Int64
	fixture := newTrendingAppFixture(t, "enabled", false, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			name := "owner/old"
			if day.Load() > 0 {
				name = "owner/new"
			}
			writeTrendingTestPage(w, trendingTestRow(name, "900", "9", r.URL.Query().Get("since")))
		case "/repos/owner/old":
			writeTrendingTestRepository(w, 301, "owner/old", 500)
		case "/repos/owner/new":
			writeTrendingTestRepository(w, 302, "owner/new", 80)
		case "/repositories/301":
			writeTrendingTestRepository(w, 301, "owner/old", 500+day.Load()*25)
		case "/repositories/302":
			writeTrendingTestRepository(w, 302, "owner/new", 80)
		default:
			http.NotFound(w, r)
		}
	})
	ctx := context.Background()
	first, err := fixture.runtime.RunDaily(ctx, false)
	if err != nil || first.Partial() || first.Fatal || first.Snapshot.SuccessCount != 1 {
		t.Fatalf("first daily = %+v %v", first, err)
	}
	fixture.clock.Add(int64(time.Minute))
	repeated, err := fixture.runtime.RunDaily(ctx, false)
	if err != nil || repeated.Partial() || repeated.Discovery.Trending == nil || !repeated.Discovery.Trending.Skipped || repeated.Snapshot.SkippedCount != 1 {
		t.Fatalf("same-day retry = %+v %v", repeated, err)
	}
	if fixture.count("/trending?since=daily") != 1 || fixture.count("/repos/owner/old") != 1 || fixture.count("/repositories/301") != 1 {
		t.Fatalf("same-day duplicated collection: %v", fixture.calls)
	}
	day.Store(1)
	fixture.clock.Add(int64(24 * time.Hour))
	next, err := fixture.runtime.RunDaily(ctx, false)
	if err != nil || next.Partial() || next.Discovery.Trending.Skipped || next.Snapshot.SuccessCount != 2 {
		t.Fatalf("next daily = %+v %v", next, err)
	}
	old, err := fixture.runtime.store.GetRepository(ctx, 301)
	if err != nil || old.MonitoringStatus != domain.MonitoringActive || domain.ShanghaiDate(old.LastDiscoveredAt) != "2026-09-07" {
		t.Fatalf("off-board project changed: %+v %v", old, err)
	}
	history, err := fixture.runtime.store.ListDailySnapshots(ctx, 301, domain.SnapshotFilter{})
	if err != nil || len(history) != 2 {
		t.Fatalf("off-board history = %+v %v", history, err)
	}
	latest, err := fixture.runtime.store.GetDailySnapshot(ctx, 301, domain.ShanghaiDate(fixture.now()))
	if err != nil || *latest.StarCount != 525 || fixture.count("/repositories/301") != 2 {
		t.Fatalf("off-board snapshot missing: %+v %v", latest, err)
	}
}

func TestTrendingPartialBoardDoesNotBlockSearchOrExistingSnapshots(t *testing.T) {
	fixture := newTrendingAppFixture(t, "enabled", true, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trending":
			period := r.URL.Query().Get("since")
			if period == "weekly" {
				http.Error(w, "upstream unavailable", http.StatusInternalServerError)
				return
			}
			name := "owner/daily"
			if period == "monthly" {
				name = "owner/monthly"
			}
			writeTrendingTestPage(w, trendingTestRow(name, "900", "9", period))
		case "/repos/owner/daily":
			writeTrendingTestRepository(w, 401, "owner/daily", 10)
		case "/repos/owner/monthly":
			writeTrendingTestRepository(w, 402, "owner/monthly", 20)
		case "/search/repositories":
			writeTrendingTestSearch(w, trendingTestRepository(403, "owner/search", 30))
		case "/repositories/401":
			writeTrendingTestRepository(w, 401, "owner/daily", 11)
		case "/repositories/402":
			writeTrendingTestRepository(w, 402, "owner/monthly", 21)
		case "/repositories/403":
			writeTrendingTestRepository(w, 403, "owner/search", 31)
		case "/repositories/404":
			writeTrendingTestRepository(w, 404, "owner/tracked", 41)
		default:
			http.NotFound(w, r)
		}
	})
	fixture.seed(t, 404, "owner/tracked", domain.DiscoverySourceLegacy)
	report, err := fixture.runtime.RunDaily(context.Background(), false)
	if err != nil || !report.Partial() || report.Fatal || report.Snapshot.SuccessCount != 4 || report.Discovery.CreatedCount != 3 || report.Discovery.Trending.FailureCount != 1 {
		t.Fatalf("partial daily = %+v %v", report, err)
	}
	if len(report.Discovery.Trending.Windows) != 3 || report.Discovery.Trending.Windows[1].Error == "" || report.Discovery.Trending.Windows[2].Error != "" || fixture.count("/search/repositories") != 1 || fixture.count("/repositories/404") != 1 {
		t.Fatalf("later stages missing: %+v", report)
	}
	complete, err := fixture.runtime.trendingCompletedToday(context.Background(), []string{"daily", "weekly", "monthly"})
	if err != nil || complete {
		t.Fatalf("partial board advanced completion: %t %v", complete, err)
	}
	run, err := fixture.runtime.store.GetJobRun(context.Background(), report.RunID)
	if err != nil || run.Status != domain.JobPartial {
		t.Fatalf("partial daily status = %+v %v", run, err)
	}
	var details DailyJobDetails
	if err := json.Unmarshal(run.Details, &details); err != nil {
		t.Fatal(err)
	}
	if details.Discovery.Trending == nil || len(details.Discovery.Trending.Windows) != 3 || details.Discovery.Trending.Windows[1].Error == "" {
		t.Fatal("partial board evidence missing from daily job")
	}
}

func TestTrendingParseAndResolutionFailuresCannotAdvanceCompletion(t *testing.T) {
	for _, failure := range []string{"malformed", "resolve-forbidden"} {
		t.Run(failure, func(t *testing.T) {
			fixture := newTrendingAppFixture(t, "enabled", false, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/trending":
					period := r.URL.Query().Get("since")
					if failure == "malformed" && period == "monthly" {
						writeTrendingTestPage(w, trendingTestRow("owner/bad", "1.2k", "2", period))
						return
					}
					writeTrendingTestPage(w, trendingTestRow("owner/good", "900", "9", period))
				case "/repos/owner/good":
					if failure == "resolve-forbidden" {
						w.Header().Set("Content-Type", "application/json")
						w.WriteHeader(http.StatusForbidden)
						fmt.Fprint(w, `{"message":"Resource not accessible"}`)
						return
					}
					writeTrendingTestRepository(w, 501, "owner/good", 55)
				default:
					http.NotFound(w, r)
				}
			})
			for attempt := 0; attempt < 2; attempt++ {
				report, err := fixture.runtime.Discover(context.Background(), DiscoverOptions{Source: "github-trending", DueOnly: true})
				if err != nil || !report.Partial() || report.Trending == nil || report.Trending.Skipped || report.Trending.FailureCount == 0 {
					t.Fatalf("failure appeared successful: %+v %v", report, err)
				}
				complete, err := fixture.runtime.trendingCompletedToday(context.Background(), []string{"daily", "weekly", "monthly"})
				if err != nil || complete {
					t.Fatalf("bad source advanced daily gate: %t %v", complete, err)
				}
				fixture.clock.Add(int64(time.Minute))
			}
			if fixture.count("/trending?since=daily") != 2 || fixture.count("/repos/owner/bad") != 0 {
				t.Fatalf("partial board incorrectly skipped or ingested: %v", fixture.calls)
			}
			if failure == "resolve-forbidden" {
				repositories, err := fixture.runtime.store.ListRepositories(context.Background(), domain.RepositoryFilter{})
				if err != nil || len(repositories) != 0 {
					t.Fatalf("unverified identity entered registry: %+v %v", repositories, err)
				}
			}
		})
	}
}

func TestTrendingDisabledAndOmittedConfigsRemainGitHubSearchCompatible(t *testing.T) {
	for _, mode := range []string{"disabled", "omitted"} {
		t.Run(mode, func(t *testing.T) {
			fixture := newTrendingAppFixture(t, mode, true, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/search/repositories":
					writeTrendingTestSearch(w, trendingTestRepository(601, "owner/search", 60))
				case "/repositories/601":
					writeTrendingTestRepository(w, 601, "owner/search", 61)
				default:
					http.NotFound(w, r)
				}
			})
			report, err := fixture.runtime.RunDaily(context.Background(), false)
			if err != nil || report.Partial() || report.Fatal || report.Discovery.Trending != nil || report.Snapshot.SuccessCount != 1 || fixture.count("/trending?since=daily") != 0 {
				t.Fatalf("legacy config broken: %+v %v", report, err)
			}
		})
	}
}

func TestTrendingDailyPlanningDoesNotRequestHTTPOrChangeBusinessFiles(t *testing.T) {
	fixture := newTrendingAppFixture(t, "enabled", true, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("planning issued HTTP request: %s", r.URL)
		http.Error(w, "unexpected", http.StatusInternalServerError)
	})
	fixture.seed(t, 701, "owner/tracked", domain.DiscoverySourceLegacy)
	if err := fixture.runtime.Close(); err != nil {
		t.Fatal(err)
	}
	fixture.runtime = nil
	before := fileHash(t, fixture.settings.DatabasePath)
	entriesBefore := directoryEntries(t, filepath.Dir(fixture.settings.DatabasePath))
	planner, err := OpenRuntimeWithOptions(context.Background(), fixture.settings, fixture.options(true))
	if err != nil {
		t.Fatal(err)
	}
	plan, planErr := planner.RunDaily(context.Background(), true)
	closeErr := planner.Close()
	if planErr != nil || closeErr != nil || !plan.DryRun || plan.Snapshot.TargetCount != 1 || plan.Discovery.Trending == nil || plan.Discovery.Trending.Skipped || len(plan.Discovery.Trending.Periods) != 3 {
		t.Fatalf("Trending plan = %+v %v %v", plan, planErr, closeErr)
	}
	if after := fileHash(t, fixture.settings.DatabasePath); after != before {
		t.Fatal("planning changed the target database")
	}
	if after := directoryEntries(t, filepath.Dir(fixture.settings.DatabasePath)); !reflect.DeepEqual(after, entriesBefore) {
		t.Fatalf("planning created business files: before=%v after=%v", entriesBefore, after)
	}
	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if len(fixture.calls) != 0 {
		t.Fatalf("planning HTTP calls: %v", fixture.calls)
	}
}

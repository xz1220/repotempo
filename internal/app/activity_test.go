package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

var activityTestTime = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func appActivityFixture(id int64) domain.RepositoryActivity {
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	latest := activityTestTime.Add(-time.Hour)
	value := domain.RepositoryActivity{RepositoryID: id, DefaultBranch: "main", HeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", WindowStart: start, WindowEnd: activityTestTime, FetchedAt: activityTestTime, LatestCommitAt: &latest, Commits: 1, ActiveDays: 1, Complete: true}
	for index := range 30 {
		value.Daily = append(value.Daily, domain.ActivityDay{Date: start.AddDate(0, 0, index).Format("2006-01-02")})
	}
	value.Daily[29].Count = 1
	return value
}

func activityRuntime(t *testing.T, serve http.HandlerFunc) *Runtime {
	t.Helper()
	server := httptest.NewServer(serve)
	t.Cleanup(server.Close)
	store, err := sqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	client, err := github.NewClient(github.ClientOptions{BaseURL: server.URL, UserAgent: "activity-test", HTTPClient: server.Client(), Now: func() time.Time { return activityTestTime }})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &Runtime{store: store, now: func() time.Time { return activityTestTime }, loaded: true, discovery: &config.Discovery{}, github: client}
	for _, id := range []int64{1, 2} {
		_, _, err := store.UpsertRepository(context.Background(), domain.RepositoryObservation{GitHubRepoID: id, FullName: fmt.Sprintf("owner/repo%d", id), Source: domain.DiscoverySourceManual, DiscoveredAt: activityTestTime.Add(-time.Hour), GitHubStatus: domain.GitHubActive, MonitoringStatus: domain.MonitoringActive})
		if err != nil {
			t.Fatal(err)
		}
	}
	return runtime
}

func TestRefreshActivityPreservesFreshCacheAndGeneratedReading(t *testing.T) {
	runtime := activityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("fresh cache requested GitHub")
		w.WriteHeader(500)
	})
	want := appActivityFixture(1)
	if err := runtime.store.PutRepositoryActivity(context.Background(), 1, want); err != nil {
		t.Fatal(err)
	}
	analysis, err := runtime.store.PutRepositoryAnalysis(context.Background(), domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "原有用途说明必须保留。", Source: "manual", AnalyzedAt: activityTestTime})
	if err != nil {
		t.Fatal(err)
	}
	report, err := runtime.RefreshActivity(context.Background(), ActivityOptions{Repository: "owner/repo1", Limit: 1, MaxPages: 3})
	if err != nil || report.SkippedCount != 1 || report.SuccessCount != 0 {
		t.Fatalf("fresh cache: %+v, %v", report, err)
	}
	detail, err := runtime.store.GetRepositoryDetail(context.Background(), 1, domain.ShanghaiDate(activityTestTime))
	if err != nil || detail.Analysis == nil || !reflect.DeepEqual(*detail.Analysis, analysis) {
		t.Fatalf("reading changed: %+v, %v", detail.Analysis, err)
	}
	repository, err := runtime.store.GetRepositoryByFullName(context.Background(), "owner/repo1")
	if err != nil || repository.Activity == nil || !reflect.DeepEqual(*repository.Activity, want) {
		t.Fatalf("activity changed: %+v, %v", repository.Activity, err)
	}
}

func TestRefreshActivityAccessFailureStopsBatchWithoutReplacingCache(t *testing.T) {
	requests := 0
	runtime := activityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Error(w, `{"message":"Forbidden"}`, http.StatusForbidden)
	})
	want := appActivityFixture(2)
	if err := runtime.store.PutRepositoryActivity(context.Background(), 2, want); err != nil {
		t.Fatal(err)
	}
	report, err := runtime.RefreshActivity(context.Background(), ActivityOptions{Limit: 2, MaxPages: 1, Force: true})
	if err != nil || requests != 1 || report.TargetCount != 2 || report.SuccessCount != 0 || len(report.Failures) != 1 || report.DeferredReason == "" {
		t.Fatalf("blocked collection: %+v requests=%d error=%v", report, requests, err)
	}
	repository, _ := runtime.store.GetRepositoryByFullName(context.Background(), "owner/repo2")
	if repository.Activity == nil || !reflect.DeepEqual(*repository.Activity, want) {
		t.Fatal("failure replaced cached facts")
	}
}

func TestRefreshActivityReservesKnownGitHubQuota(t *testing.T) {
	requests := 0
	runtime := activityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "99")
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(activityTestTime.Add(time.Hour).Unix()))
		w.WriteHeader(http.StatusNotModified)
	})
	if _, err := runtime.github.FetchRepositoryByID(context.Background(), 1, "fixture"); err != nil {
		t.Fatal(err)
	}
	report, err := runtime.RefreshActivity(context.Background(), ActivityOptions{Limit: 2, MaxPages: 3})
	if err != nil || requests != 1 || report.SkippedCount != 2 || report.DeferredReason == "" {
		t.Fatalf("quota guard: %+v, %v", report, err)
	}
}

func TestRefreshActivityPersistsGitHubFactsWithoutRewritingMetadata(t *testing.T) {
	requests := 0
	runtime := activityRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/repositories/1" {
			_, _ = w.Write([]byte(`{"id":1,"full_name":"owner/renamed","private":false,"fork":false,"default_branch":"main"}`))
			return
		}
		if r.URL.Path != "/repos/owner/renamed/commits" {
			t.Errorf("unverified API path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode([]any{map[string]any{"sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "commit": map[string]any{"committer": map[string]string{"date": "2026-09-08T11:00:00Z"}}}})
	})
	before, _ := runtime.store.GetRepositoryByFullName(context.Background(), "owner/repo1")
	report, err := runtime.RefreshActivity(context.Background(), ActivityOptions{Repository: "owner/repo1", Limit: 1, MaxPages: 3})
	if err != nil || requests != 3 || report.SuccessCount != 1 || len(report.Failures) != 0 {
		t.Fatalf("successful collection: %+v requests=%d error=%v", report, requests, err)
	}
	after, err := runtime.store.GetRepositoryByFullName(context.Background(), "owner/repo1")
	if err != nil || after.Activity == nil || after.Activity.Commits != 1 || after.Activity.ActiveDays != 1 || !after.Activity.Complete {
		t.Fatalf("stored facts: %+v, %v", after.Activity, err)
	}
	after.Activity = nil
	if !reflect.DeepEqual(before, after) {
		t.Fatal("commit collection rewrote metadata, classification or timestamps")
	}
}

type activityCommandFake struct {
	fakeCommandApplication
	options ActivityOptions
}

func (app *activityCommandFake) RefreshActivity(_ context.Context, options ActivityOptions) (ActivityReport, error) {
	app.options = options
	return ActivityReport{SuccessCount: 1}, nil
}

func TestCLIActivityBoundsAndDateMeaning(t *testing.T) {
	for _, args := range [][]string{{"activity"}, {"activity", "refresh", "--limit", "501"}, {"activity", "refresh", "--max-pages", "0"}, {"activity", "refresh", "--date", "2026-02-30"}, {"activity", "refresh", "--date", "2026-09-08", "--repository", "owner/repo"}} {
		app := &activityCommandFake{}
		cli, _, _ := testCLI(app)
		if code := cli.Run(context.Background(), args); code != ExitUsage || app.closed {
			t.Fatalf("invalid arguments reached runtime: %v code=%d", args, code)
		}
	}
	app := &activityCommandFake{}
	cli, _, _ := testCLI(app)
	if code := cli.Run(context.Background(), []string{"--json", "activity", "refresh", "--date", "2026-09-08", "--limit", "300", "--max-pages", "5"}); code != ExitSuccess {
		t.Fatalf("valid args: %d", code)
	}
	if app.options.Date != "2026-09-08" || app.options.Limit != 300 || app.options.MaxPages != 5 || !app.closed {
		t.Fatalf("lost options: %+v", app.options)
	}
}

func TestWebActivityMappingHasNoSharedMutablePointers(t *testing.T) {
	value := appActivityFixture(42)
	mapped := mapRepositoryActivity(&value)
	mapped.Daily[0].Count = 99
	*mapped.LatestCommitAt = time.Time{}
	if value.Daily[0].Count != 0 || value.LatestCommitAt.IsZero() || mapRepositoryActivity(nil) != nil {
		t.Fatal("web mapping changed source facts")
	}
}

func TestDailyActivityFailuresRemainVisibleWithoutBeingFatal(t *testing.T) {
	report := DailyReport{Activity: &ActivityReport{Failures: []OperationFailure{{Stage: "activity", Message: "GitHub unavailable"}}}}
	if report.Fatal || !report.Partial() || summarizeDailyErrors(report) != "1 daily operations failed" {
		t.Fatalf("supplementary failure hidden or made fatal: %+v", report)
	}
	if dailyDetails(report, nil).Activity != report.Activity {
		t.Fatal("collection history omitted activity report")
	}
}

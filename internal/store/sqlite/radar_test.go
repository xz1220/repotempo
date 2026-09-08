package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func TestRadarOverviewSeparatesGrowthSlowdownAndMissingObservations(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 10; id++ {
		discovered := testNow.AddDate(0, 0, -60)
		if id == 8 || id == 9 {
			discovered = testNow
		}
		if id == 10 {
			discovered = testNow.AddDate(0, 0, 1)
		}
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/repo-%d", id), domain.DiscoverySourceGitHubSearch, discovered)
	}
	for _, values := range []struct{ id, before, baseline, current int64 }{
		{1, 100, 110, 150}, {2, 100, 150, 160}, {3, 200, 220, 220}, {4, 100, 100, 90},
	} {
		putSuccess(t, store, values.id, "2026-08-16", values.before)
		putSuccess(t, store, values.id, "2026-08-23", values.baseline)
		putSuccess(t, store, values.id, "2026-08-30", values.current)
	}
	putSuccess(t, store, 5, "2026-08-22", 900) // Near the boundary is not the boundary.
	putSuccess(t, store, 5, "2026-08-30", 999)
	putSuccess(t, store, 6, "2026-08-23", 15)
	putSuccess(t, store, 6, "2026-08-29", 16)
	putFailure(t, store, 6, "2026-08-30")
	putFailure(t, store, 8, "2026-08-30")
	putSuccess(t, store, 9, "2026-08-30", 5)
	putSuccess(t, store, 10, "2026-08-30", 999999) // A later discovery cannot enter the earlier scope.
	putSuccess(t, store, 6, "2026-08-31", 10000)   // Nor can a later observation replace the historical last known value.

	value, err := store.RadarOverview(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	wantCoverage := domain.RadarCoverage{
		ComparisonCoverage: domain.ComparisonCoverage{
			BaselineDate: date("2026-08-23"), AsOfDate: date("2026-08-30"),
			ScopeCount: 9, ObservedCount: 6, ComparableCount: 4, NewCount: 2,
		},
		FailedCount: 2, MissingCount: 1, StaleCount: 1,
		UpCount: 2, FlatCount: 1, DownCount: 1, MomentumComparableCount: 4, SlowingCount: 3,
	}
	if value.Coverage != wantCoverage {
		t.Fatalf("coverage = %+v, want %+v", value.Coverage, wantCoverage)
	}
	assertRadarIDs(t, value.Fastest, []int64{1, 2})
	assertRadarIDs(t, value.FallingBehind, []int64{2, 3, 4})
	if len(value.Slowest) != 0 || len(value.NewRepositories) != 0 || len(value.History) != 0 {
		t.Fatalf("overview loaded unused lists or history: %+v", value)
	}
	if *value.FallingBehind[0].PreviousDelta != 50 || *value.FallingBehind[0].StarDelta != 10 || *value.FallingBehind[0].MomentumChange != -40 {
		t.Fatalf("incorrect window comparison: %+v", value.FallingBehind[0])
	}
	page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7, Search: "repo-6"})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("stale repository disappeared: %+v, %v", page, err)
	}
	stale := page.Items[0]
	if stale.CurrentStars != nil || !stale.IsStale || stale.LastObservedDate == nil || *stale.LastObservedDate != date("2026-08-29") ||
		stale.LastObservedStars == nil || *stale.LastObservedStars != 16 || stale.StarDelta != nil || stale.MomentumChange != nil {
		t.Fatalf("historical stale values are wrong: %+v", stale)
	}
}

func TestRadarHistoryUsesOneCohortAndPreservesIncompleteDays(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/repo-%d", id), domain.DiscoverySourceGitHubSearch, testNow.AddDate(0, 0, -60))
	}
	for _, values := range []struct{ id, baseline, current int64 }{{1, 0, 10}, {2, 0, 20}} {
		putSuccess(t, store, values.id, "2026-08-23", values.baseline)
		putSuccess(t, store, values.id, "2026-08-30", values.current)
	}
	putSuccess(t, store, 1, "2026-08-24", 1)
	putSuccess(t, store, 3, "2026-08-24", 999999) // An outside project cannot fill a missing cohort member.
	putSuccess(t, store, 1, "2026-08-25", 3)
	putSuccess(t, store, 2, "2026-08-25", 4)
	value, err := store.RadarOverview(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7})
	if err != nil {
		t.Fatal(err)
	}
	if len(value.History) != 0 {
		t.Fatal("overview must not execute its legacy history query")
	}
	// The explicit history helper retains its original evidence contract for
	// callers which request it separately; it is no longer on the overview path.
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7}
	scope, arguments := trendScope(query)
	history, err := store.radarHistory(ctx, query, date("2026-08-23"), scope, arguments)
	if err != nil {
		t.Fatal(err)
	}
	if history[0].Stars == nil || *history[0].Stars != 0 || history[0].Index != nil ||
		history[1].Stars != nil || history[1].ObservedCount != 1 || history[1].CohortCount != 2 ||
		history[2].Stars == nil || *history[2].Stars != 7 || history[2].Index != nil {
		t.Fatalf("zero baseline or incomplete day was fabricated: %+v", history)
	}
	if len(value.FallingBehind) != 0 || value.Coverage.MomentumComparableCount != 0 {
		t.Fatalf("missing third endpoint created a slowdown: %+v", value)
	}
}

func TestRadarEmptyAndSingleRepositoryWindows(t *testing.T) {
	for _, days := range []int{1, 7, 30} {
		t.Run(fmt.Sprintf("%d-day", days), func(t *testing.T) {
			store, _ := newTestStore(t)
			query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: days}
			empty, err := store.RadarOverview(context.Background(), query)
			if err != nil || empty.Coverage.ScopeCount != 0 || len(empty.History) != 0 || len(empty.Slowest) != 0 || len(empty.NewRepositories) != 0 {
				t.Fatalf("empty overview: %+v, %v", empty, err)
			}
			for _, point := range empty.History {
				if point.Stars != nil || point.Index != nil || point.CohortCount != 0 {
					t.Fatalf("empty overview fabricated history: %+v", point)
				}
			}
			addRepositoryFromSource(t, store, 1, "owner/solo", domain.DiscoverySourceManual, testNow.AddDate(0, 0, -90))
			baseline, _ := query.AsOf.AddDays(-days)
			previous, _ := baseline.AddDays(-days)
			putSuccess(t, store, 1, previous.String(), 100)
			putSuccess(t, store, 1, baseline.String(), 120)
			putSuccess(t, store, 1, query.AsOf.String(), 130)
			value, err := store.RadarOverview(context.Background(), query)
			if err != nil || value.Coverage.ComparableCount != 1 || value.Coverage.SlowingCount != 1 ||
				len(value.FallingBehind) != 1 || value.FallingBehind[0].DailyVelocity == nil || math.Abs(*value.FallingBehind[0].DailyVelocity-10.0/float64(days)) > 1e-9 {
				t.Fatalf("single repository overview = %+v, %v", value, err)
			}
		})
	}
}

func TestTrendPaginationKeepsMissingRowsAndStableTies(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 7; id++ {
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/repo-%d", id), domain.DiscoverySourceManual, testNow.Add(-time.Hour))
		if id < 4 {
			putSuccess(t, store, id, "2026-08-29", 100)
			putSuccess(t, store, id, "2026-08-30", 100)
		} else if id < 6 {
			putSuccess(t, store, id, "2026-08-29", 100)
		}
	}
	for _, sort := range []domain.RepositoryTrendSort{domain.RepositoryTrendSortDelta, domain.RepositoryTrendSortStars, domain.RepositoryTrendSortNewest, domain.RepositoryTrendSortLowGrowth, domain.RepositoryTrendSortSlowdown} {
		query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 1, Sort: sort, Limit: 2}
		ids := []int64{}
		for pages := 0; pages < 5; pages++ {
			page, err := store.ListRepositoryTrends(ctx, query)
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range page.Items {
				ids = append(ids, item.Repository.GitHubRepoID)
			}
			if !page.HasMore {
				break
			}
			id := page.Items[len(page.Items)-1].Repository.GitHubRepoID
			query.AfterID = &id
		}
		want := []int64{1, 2, 3, 4, 5, 6, 7}
		if sort == domain.RepositoryTrendSortLowGrowth {
			want = []int64{1, 2, 3}
		}
		if sort == domain.RepositoryTrendSortSlowdown {
			want = []int64{}
		}
		if !reflect.DeepEqual(ids, want) {
			t.Fatalf("sort %s lost or duplicated tied/missing rows: %v", sort, ids)
		}
	}
}

func TestRadarNewDiscoveryUsesShanghaiFirstSeenDate(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	createdYearsAgo := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	createdToday := testNow
	for _, observation := range []domain.RepositoryObservation{
		// 16:00 UTC is midnight on the selected Shanghai date. An established
		// repository can be newly discovered even when its first fetch fails.
		{GitHubRepoID: 1, FullName: "owner/old-but-newly-seen", GitHubCreatedAt: &createdYearsAgo,
			Source: domain.DiscoverySourceManual, DiscoveredAt: time.Date(2026, 8, 29, 16, 0, 0, 0, time.UTC)},
		{GitHubRepoID: 2, FullName: "owner/seen-yesterday", GitHubCreatedAt: &createdToday,
			Source: domain.DiscoverySourceManual, DiscoveredAt: time.Date(2026, 8, 29, 15, 59, 59, 0, time.UTC)},
		{GitHubRepoID: 3, FullName: "owner/seen-tomorrow", GitHubCreatedAt: &createdYearsAgo,
			Source: domain.DiscoverySourceManual, DiscoveredAt: time.Date(2026, 8, 30, 16, 0, 0, 0, time.UTC)},
	} {
		if _, _, err := store.UpsertRepository(ctx, observation); err != nil {
			t.Fatal(err)
		}
	}
	putFailure(t, store, 1, "2026-08-30")
	value, err := store.RadarOverview(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 1})
	if err != nil {
		t.Fatal(err)
	}
	if value.Coverage.NewCount != 1 || value.Coverage.ScopeCount != 2 || value.Coverage.ObservedCount != 0 {
		t.Fatalf("new discovery date was confused with repository creation or success: %+v", value.Coverage)
	}
	if len(value.NewRepositories) != 0 {
		t.Fatal("overview fetched new-project cards which belong to the discovery feed")
	}
	page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 1, OnlyNew: true, Sort: domain.RepositoryTrendSortNewest})
	if err != nil {
		t.Fatal(err)
	}
	assertRadarIDs(t, page.Items, []int64{1})
	if page.Items[0].Repository.GitHubCreatedAt == nil || !page.Items[0].Repository.GitHubCreatedAt.Equal(createdYearsAgo) {
		t.Fatal("GitHub creation date was lost")
	}
}

func TestRadarOverviewReturnsTenPositiveLeadersAndSixSlowdowns(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 21; id++ {
		firstSeen := testNow.AddDate(0, 0, -60)
		if id == 21 {
			firstSeen = testNow
		}
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/leader-%02d", id), domain.DiscoverySourceGitHubSearch, firstSeen)
	}
	for id := int64(1); id <= 17; id++ {
		putSuccess(t, store, id, "2026-08-16", 100)
		putSuccess(t, store, id, "2026-08-23", 200)
		stars := int64(200) + id
		if id == 16 {
			stars = 200
		}
		if id == 17 {
			stars = 150
		}
		putSuccess(t, store, id, "2026-08-30", stars)
	}
	putSuccess(t, store, 18, "2026-08-30", 999999) // Large, but missing a comparison baseline.
	putSuccess(t, store, 19, "2026-08-23", 100)
	putFailure(t, store, 19, "2026-08-30")
	putSuccess(t, store, 20, "2026-08-23", 100)
	putSuccess(t, store, 20, "2026-08-30", 99) // Negative; missing the third endpoint for slowdown.
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7, Limit: 1}
	value, err := store.RadarOverview(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	assertRadarIDs(t, value.Fastest, []int64{15, 14, 13, 12, 11, 10, 9, 8, 7, 6})
	assertRadarIDs(t, value.FallingBehind, []int64{17, 16, 1, 2, 3, 4})
	if value.Coverage.NewCount != 1 || value.Coverage.UpCount != 15 || value.Coverage.FlatCount != 1 || value.Coverage.DownCount != 2 || value.Coverage.SlowingCount != 17 {
		t.Fatalf("coverage changed with board limits: %+v", value.Coverage)
	}
	if value.Slowest == nil || value.NewRepositories == nil || value.History == nil || len(value.Slowest)+len(value.NewRepositories)+len(value.History) != 0 {
		t.Fatal("deprecated overview fields must remain empty arrays")
	}
	for _, entry := range value.Fastest {
		if entry.StarDelta == nil || *entry.StarDelta <= 0 {
			t.Fatalf("non-positive or missing growth in top ten: %+v", entry)
		}
	}
}

func TestNewFocusedRepositoryPaginationDoesNotRequireSnapshots(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 7; id++ {
		focus := id%2 == 0
		_, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: id, FullName: fmt.Sprintf("owner/new-%d", id), Source: domain.DiscoverySourceManual,
			DiscoveredAt: testNow.Add(time.Duration(id) * time.Minute), IsFocus: &focus,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	query := domain.RepositoryTrendQuery{
		AsOf: date("2026-08-30"), WindowDays: 7, Sort: domain.RepositoryTrendSortNewest,
		OnlyNew: true, OnlyFocus: true, Limit: 2,
	}
	first, err := store.ListRepositoryTrends(ctx, query)
	if err != nil || first.Total != 3 || !first.HasMore || first.Coverage.ScopeCount != 3 || first.Coverage.NewCount != 3 || first.Coverage.ObservedCount != 0 {
		t.Fatalf("new focus first page: %+v, %v", first, err)
	}
	assertRadarIDs(t, first.Items, []int64{6, 4})
	query.AfterID = pointer(int64(4))
	next, err := store.ListRepositoryTrends(ctx, query)
	if err != nil || next.Total != 3 || next.HasMore || len(next.Items) != 1 || !next.Items[0].IsStale || next.Items[0].LastObservedStars != nil {
		t.Fatalf("new focus next page: %+v, %v", next, err)
	}
	assertRadarIDs(t, next.Items, []int64{2})
}

func TestPreferredSnapshotDateIgnoresManualSnapshotsAndKeepsFailedBatches(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := store.PreferredSnapshotDate(ctx); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatalf("empty store preferred date error = %v", err)
	}
	addRepository(t, store, 1, "owner/manual")
	putSuccess(t, store, 1, "2026-08-30", 10)
	assertDate := func(want string) {
		t.Helper()
		got, err := store.PreferredSnapshotDate(ctx)
		if err != nil || got != date(want) {
			t.Fatalf("preferred date = %s, %v; want %s", got, err, want)
		}
	}
	assertDate("2026-08-30") // Legacy imports with no batch log still work.
	createRun := func(id, kind string, status domain.JobStatus, started time.Time, details string) {
		t.Helper()
		var finished *time.Time
		if status != domain.JobRunning {
			value := started.Add(time.Hour)
			finished = &value
		}
		if err := store.CreateJobRun(ctx, domain.JobRun{
			RunID: id, JobType: kind, Status: status, StartedAt: started, FinishedAt: finished, Details: json.RawMessage(details),
		}); err != nil {
			t.Fatal(err)
		}
	}
	createRun("daily", "run-daily", domain.JobSuccess, testNow.AddDate(0, 0, -1), `{"snapshot":{"date":"2026-08-29"}}`)
	assertDate("2026-08-29") // A manual observation on the 30th does not shift the dashboard.
	createRun("running", "snapshot", domain.JobRunning, testNow, `{}`)
	assertDate("2026-08-29")
	createRun("partial", "snapshot", domain.JobPartial, testNow, `{"date":"2026-08-30"}`)
	assertDate("2026-08-30")
	createRun("failed-before-report", "run-daily", domain.JobFailed, testNow.AddDate(0, 0, 1), `{"snapshot":{"date":""}}`)
	assertDate("2026-08-31")
	createRun("backfill", "snapshot", domain.JobSuccess, testNow.AddDate(0, 0, 2), `{"date":"2026-08-28"}`)
	assertDate("2026-08-31") // A historical rerun cannot roll the default date backwards.
	createRun("watch", "watch-add", domain.JobSuccess, testNow.AddDate(0, 0, 3), `{"date":"2026-09-02"}`)
	assertDate("2026-08-31")
}

func TestLatestLibraryDateIncludesNewUnobservedProjectsAndFailedBatches(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if _, err := store.LatestLibraryDate(ctx); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatalf("empty library date error = %v", err)
	}
	addRepositoryFromSource(t, store, 1, "owner/existing", domain.DiscoverySourceGitHubSearch, testNow.AddDate(0, 0, -30))
	putSuccess(t, store, 1, "2026-09-06", 100)
	assertLibraryDate := func(want string) {
		t.Helper()
		actual, err := store.LatestLibraryDate(ctx)
		if err != nil || actual != date(want) {
			t.Fatalf("library date = %s, %v; want %s", actual, err, want)
		}
	}
	assertLibraryDate("2026-09-06")
	today := time.Date(2026, 9, 7, 4, 0, 0, 0, time.UTC)
	addRepositoryFromSource(t, store, 2, "owner/newly-imported", domain.DiscoverySourceLegacy, today)
	assertLibraryDate("2026-09-07")
	selected, err := store.LatestLibraryDate(ctx)
	if err != nil {
		t.Fatal(err)
	}
	page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: selected, OnlyNew: true, Sort: domain.RepositoryTrendSortNewest})
	if err != nil || len(page.Items) != 1 || page.Items[0].Repository.GitHubRepoID != 2 || page.Items[0].CurrentStars != nil || !page.Items[0].IsStale {
		t.Fatalf("unobserved new project hidden from library: %+v, %v", page, err)
	}
	putFailure(t, store, 2, "2026-09-07")
	assertLibraryDate("2026-09-07")
	// The next collection can fail for every project without creating a new
	// registry entry or successful snapshot. The library must still show its
	// true collection date, while retaining each project's last known stars.
	next := today.AddDate(0, 0, 1)
	finished := next.Add(time.Minute)
	if err := store.CreateJobRun(ctx, domain.JobRun{
		RunID: "fully-failed", JobType: "run-daily", Status: domain.JobFailed,
		StartedAt: next, FinishedAt: &finished, TargetCount: 2, FailureCount: 2,
		Details: json.RawMessage(`{"snapshot":{"date":"2026-09-08"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	assertLibraryDate("2026-09-08")
	putSuccess(t, store, 2, "2026-09-09", 5)
	assertLibraryDate("2026-09-09") // A newer manual success is also immediately available.
}

func TestRadarOnlyFocusAndTopicScope(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/repo-%d", id), domain.DiscoverySourceManual, testNow.AddDate(0, 0, -30))
		putSuccess(t, store, id, "2026-08-29", id*10)
		putSuccess(t, store, id, "2026-08-30", id*20)
	}
	_, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 1, FullName: "owner/repo-1", Source: domain.DiscoverySourceManual, IsFocus: pointer(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	parent, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "parent", Name: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "child", Name: "Child", ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: 1, TopicID: child.ID, Source: domain.TopicSourceManual}); err != nil {
		t.Fatal(err)
	}
	for _, query := range []domain.RepositoryTrendQuery{
		{AsOf: date("2026-08-30"), WindowDays: 1, OnlyFocus: true},
		{AsOf: date("2026-08-30"), WindowDays: 1, TopicSlug: "parent"},
	} {
		value, err := store.RadarOverview(ctx, query)
		if err != nil || value.Coverage.ScopeCount != 1 || value.Coverage.ComparableCount != 1 {
			t.Fatalf("scope not reflected in all overview values: %+v, %v", value, err)
		}
		assertRadarIDs(t, value.Fastest, []int64{1})
	}
}

func assertRadarIDs(t *testing.T, values []domain.RepositoryTrendMetric, want []int64) {
	t.Helper()
	actual := make([]int64, 0, len(values))
	for _, item := range values {
		actual = append(actual, item.Repository.GitHubRepoID)
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("repository IDs = %v, want %v", actual, want)
	}
}

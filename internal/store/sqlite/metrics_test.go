package sqlite

import (
	"context"
	"math"
	"testing"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestDashboardAndTopicMetricsRespectFailuresAndMissingDates(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id, name := range []string{"owner/one", "owner/two", "owner/three", "owner/missing"} {
		addRepository(t, store, int64(id+1), name)
	}
	// Repo one has exact 7/30-day baselines.
	putSuccess(t, store, 1, "2026-07-31", 100)
	putSuccess(t, store, 1, "2026-08-23", 120)
	putSuccess(t, store, 1, "2026-08-29", 130)
	putSuccess(t, store, 1, "2026-08-30", 140)
	// Repo two's closest real baselines precede the target dates. The failed
	// observation is retained but never participates in a delta.
	putSuccess(t, store, 2, "2026-07-20", 200)
	putSuccess(t, store, 2, "2026-08-22", 220)
	putFailure(t, store, 2, "2026-08-29")
	putSuccess(t, store, 2, "2026-08-30", 230)
	// Repo three failed today, so its last known stars remain visible but all
	// today-based deltas are NULL.
	putSuccess(t, store, 3, "2026-08-29", 50)
	putFailure(t, store, 3, "2026-08-30")
	// Repo four has no observations at all and remains an explicit coverage gap.

	summary, err := store.DashboardSummary(ctx, date("2026-08-30"))
	if err != nil {
		t.Fatalf("dashboard summary: %v", err)
	}
	if summary.RepositoryCount != 4 || summary.ActiveCount != 4 {
		t.Fatalf("repository totals = %d/%d", summary.RepositoryCount, summary.ActiveCount)
	}
	if summary.Coverage.TargetCount != 4 || summary.Coverage.SuccessCount != 2 ||
		summary.Coverage.FailureCount != 1 || summary.Coverage.MissingCount != 1 {
		t.Fatalf("coverage = %+v", summary.Coverage)
	}
	if math.Abs(summary.Coverage.Percent-50) > 0.0001 {
		t.Fatalf("coverage percent = %f, want 50", summary.Coverage.Percent)
	}
	assertGrowth(t, summary.Growth, 420, 20, 30, 70)

	metrics, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:       date("2026-08-30"),
		Sort:       domain.RepositorySortDay,
		Descending: true,
	})
	if err != nil {
		t.Fatalf("list repository metrics: %v", err)
	}
	byID := make(map[int64]domain.RepositoryMetric)
	for _, metric := range metrics {
		byID[metric.Repository.GitHubRepoID] = metric
	}
	assertGrowth(t, byID[1].Growth, 140, 10, 20, 40)
	assertGrowth(t, byID[2].Growth, 230, 10, 10, 30)
	if byID[3].Growth.Current == nil || *byID[3].Growth.Current != 50 ||
		byID[3].Growth.Day != nil || byID[3].Growth.SevenDay != nil || byID[3].Growth.ThirtyDay != nil {
		t.Fatalf("failed-current repo growth = %+v", byID[3].Growth)
	}
	if byID[4].Growth.Current != nil {
		t.Fatalf("never-observed repo current = %v, want nil", byID[4].Growth.Current)
	}

	topic, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "agents", Name: "Agents"})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	for _, repositoryID := range []int64{1, 2, 3} {
		if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
			RepositoryID: repositoryID,
			TopicID:      topic.ID,
			Source:       domain.TopicSourceManual,
		}); err != nil {
			t.Fatalf("assign repo %d to topic: %v", repositoryID, err)
		}
	}
	topicDetails, err := store.GetTopicDetail(ctx, "agents", date("2026-08-30"), false)
	if err != nil {
		t.Fatalf("topic detail: %v", err)
	}
	if topicDetails.Metric.RepositoryCount != 3 {
		t.Fatalf("topic repository count = %d", topicDetails.Metric.RepositoryCount)
	}
	assertGrowth(t, topicDetails.Metric.Growth, 420, 20, 30, 70)
	if topicDetails.Metric.LeaderRepositoryID == nil || *topicDetails.Metric.LeaderRepositoryID != 2 {
		t.Fatalf("topic leader = %+v", topicDetails.Metric)
	}
	if topicDetails.Metric.Concentration == nil || math.Abs(*topicDetails.Metric.Concentration-float64(230)/420) > 0.0001 {
		t.Fatalf("topic concentration = %v", topicDetails.Metric.Concentration)
	}
	lastPoint := topicDetails.History[len(topicDetails.History)-1]
	if lastPoint.Date != date("2026-08-30") || lastPoint.StarCount == nil || *lastPoint.StarCount != 370 ||
		lastPoint.ObservedCount != 2 || lastPoint.TargetCount != 3 {
		t.Fatalf("topic history point = %+v", lastPoint)
	}

	excluded, err := store.GetTopicDetail(ctx, "agents", date("2026-08-30"), true)
	if err != nil {
		t.Fatalf("topic detail excluding leader: %v", err)
	}
	if excluded.ExcludedRepositoryID == nil || *excluded.ExcludedRepositoryID != 2 || excluded.Metric.RepositoryCount != 2 {
		t.Fatalf("excluded topic metadata = %+v", excluded)
	}
	assertGrowth(t, excluded.Metric.Growth, 190, 10, 20, 40)
	excludedLast := excluded.History[len(excluded.History)-1]
	if excludedLast.StarCount == nil || *excludedLast.StarCount != 140 ||
		excludedLast.ObservedCount != 1 || excludedLast.TargetCount != 2 {
		t.Fatalf("excluded topic history point = %+v", excludedLast)
	}

	detail, err := store.GetRepositoryDetail(ctx, 3, date("2026-08-30"))
	if err != nil {
		t.Fatalf("repository detail: %v", err)
	}
	if detail.ValidFrom == nil || *detail.ValidFrom != date("2026-08-29") || len(detail.FailedDates) != 1 {
		t.Fatalf("repository validity/failures = %+v", detail)
	}
}

func assertGrowth(t *testing.T, growth domain.GrowthMetric, current, day, sevenDay, thirtyDay int64) {
	t.Helper()
	if growth.Current == nil || *growth.Current != current ||
		growth.Day == nil || *growth.Day != day ||
		growth.SevenDay == nil || *growth.SevenDay != sevenDay ||
		growth.ThirtyDay == nil || *growth.ThirtyDay != thirtyDay {
		t.Fatalf("growth = %+v, want current=%d day=%d seven=%d thirty=%d",
			growth, current, day, sevenDay, thirtyDay)
	}
}

func TestRepositoryMetricsRequireARealBaselineAndApplyFilters(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "alpha/only-snapshot")
	addRepository(t, store, 2, "beta/other")
	putSuccess(t, store, 1, "2026-08-30", 10)
	putSuccess(t, store, 2, "2026-08-29", 20)
	putSuccess(t, store, 2, "2026-08-30", 22)

	metric, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:         date("2026-08-30"),
		RepositoryID: pointer(int64(1)),
	})
	if err != nil || len(metric) != 1 {
		t.Fatalf("single-snapshot metric = (%+v, %v)", metric, err)
	}
	if metric[0].Growth.Current == nil || *metric[0].Growth.Current != 10 ||
		metric[0].Growth.Day != nil || metric[0].Growth.SevenDay != nil || metric[0].Growth.ThirtyDay != nil {
		t.Fatalf("single snapshot fabricated growth: %+v", metric[0].Growth)
	}

	topic, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "selected", Name: "Selected"})
	if err != nil {
		t.Fatalf("create filter topic: %v", err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
		RepositoryID: 2,
		TopicID:      topic.ID,
		Source:       domain.TopicSourceManual,
	}); err != nil {
		t.Fatalf("assign filter topic: %v", err)
	}
	filtered, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:      date("2026-08-30"),
		Search:    "BETA/",
		TopicSlug: "selected",
		Sort:      domain.RepositorySortStars,
	})
	if err != nil || len(filtered) != 1 || filtered[0].Repository.GitHubRepoID != 2 {
		t.Fatalf("filtered metrics = (%+v, %v)", filtered, err)
	}
	injection, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:   date("2026-08-30"),
		Search: `%' OR 1=1 --`,
	})
	if err != nil || len(injection) != 0 {
		t.Fatalf("parameterized search = (%+v, %v), want no matches", injection, err)
	}
}

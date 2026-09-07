package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebAdapterMapsTrendingEvidenceForDiscoveryAndDailyRuns(t *testing.T) {
	for _, raw := range []string{
		`{"trending":{"windows":[{"period":"daily","entries":[{"full_name":"owner/repo"}]},{"period":"weekly","error":"HTTP 403","entries":[]}]}}`,
		`{"discovery":{"trending":{"windows":[{"period":"daily","entries":[{"full_name":"owner/repo"}]},{"period":"weekly","error":"HTTP 403","entries":[]}]}}}`,
	} {
		var run web.JobRun
		applyJobDetails(&run, json.RawMessage(raw))
		if len(run.TrendingWindows) != 2 || run.TrendingWindows[0].Count != 1 || run.TrendingWindows[1].Error != "HTTP 403" {
			t.Fatalf("Trending evidence not mapped: %+v", run)
		}
	}
	var skipped web.JobRun
	applyJobDetails(&skipped, json.RawMessage(`{"trending":{"skipped":true,"skip_reason":"already captured and resolved today"}}`))
	if !skipped.TrendingSkipped || skipped.TrendingSkipReason == "" {
		t.Fatal("Trending skip status not mapped")
	}
}

func TestWebAdapterMapsStoreEvidenceWithoutLeakingInternalErrors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/radar.db", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	parent, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "ai-agent", Name: "AI Agent", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	child, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "memory", Name: "Memory", ParentID: &parent.ID, Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	for id, name := range map[int64]string{1: "owner/leader", 2: "owner/peer"} {
		_, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: id, FullName: name, HTMLURL: "https://github.com/" + name,
			Source: domain.DiscoverySourceGitHubSearch, Profile: "topic-popular",
			DiscoveredAt: now.AddDate(0, 0, -5), MonitoringStatus: domain.MonitoringActive,
			GitHubStatus: domain.GitHubActive,
		})
		if err != nil {
			t.Fatal(err)
		}
		confidence := 0.8
		if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
			RepositoryID: id, TopicID: child.ID, Source: domain.TopicSourceGitHub,
			Confidence: &confidence, AssignedAt: now,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put := func(id, stars int64, date string) {
		snapshotDate, _ := domain.ParseDate(date)
		if _, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
			RepositoryID: id, SnapshotDate: snapshotDate, CapturedAt: now,
			StarCount: &stars, FetchStatus: domain.FetchSuccess,
		}); err != nil {
			t.Fatal(err)
		}
	}
	put(1, 100, "2026-08-29")
	put(2, 50, "2026-08-29")
	put(1, 125, "2026-08-30")
	details, _ := json.Marshal(map[string]any{
		"failures": []map[string]string{{"target": "/var/lib/github-radar/private.db"}, {"target": "owner/peer"}},
		"profiles": []map[string]any{{"name": "topic-popular", "hit_count": 2, "incomplete_results": true, "split_count": 3}},
	})
	finished := now.Add(time.Minute)
	if err := store.CreateJobRun(ctx, domain.JobRun{
		RunID: "run-1", JobType: "discover", StartedAt: now, FinishedAt: &finished,
		Status: domain.JobPartial, FailureCount: 1, Details: details,
		ErrorSummary: "open /var/lib/github-radar/private.db: SQL table repositories failed", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	adapter := WebAdapter{Store: store}
	dashboard, err := adapter.DashboardSummary(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(dashboard.GrowthHistory) != 30 {
		t.Fatalf("growth history points = %d, want 30", len(dashboard.GrowthHistory))
	}
	latestGrowth := dashboard.GrowthHistory[len(dashboard.GrowthHistory)-1]
	if latestGrowth.Delta == nil || *latestGrowth.Delta != 25 || latestGrowth.ComparableRepositoryCount != 1 || latestGrowth.GapSpanningRepositoryCount != 0 {
		t.Fatalf("latest growth history = %#v", latestGrowth)
	}
	repositories, err := adapter.ListRepositoryMetrics(ctx, structRepositoryQuery(now))
	if err != nil || repositories.Total != 2 {
		t.Fatalf("repositories = (%#v, %v)", repositories, err)
	}
	if _, err := adapter.GetRepositoryDetail(ctx, 1, now); err != nil {
		t.Fatal(err)
	}
	if topics, err := adapter.ListTopicMetrics(ctx, now); err != nil || len(topics.Items) != 2 {
		t.Fatalf("topics = (%#v, %v)", topics, err)
	} else if topics.Classification.RepositoryCount != 2 || topics.Classification.ClassifiedCount != 2 ||
		topics.Classification.UnclassifiedCount != 0 || topics.Items[0].Comparable1D != 1 {
		t.Fatalf("topic coverage = %#v items=%#v", topics.Classification, topics.Items)
	}
	topicDetail, err := adapter.GetTopicDetail(ctx, "memory", now, false)
	if err != nil {
		t.Fatal(err)
	}
	if topicDetail.ConcentrationPercent == nil || *topicDetail.ConcentrationPercent <= 0 || *topicDetail.ConcentrationPercent > 100 {
		t.Fatalf("concentration = %#v", topicDetail.ConcentrationPercent)
	}
	if len(topicDetail.History) != 2 || topicDetail.History[0].Stars == nil || *topicDetail.History[0].Stars != 100 ||
		topicDetail.History[1].Stars == nil || *topicDetail.History[1].Stars != 125 {
		t.Fatalf("fixed topic cohort history = %#v", topicDetail.History)
	}
	discoveries, err := adapter.DiscoverySummary(ctx)
	if err != nil || len(discoveries.Profiles) == 0 || !discoveries.Profiles[0].IncompleteResults {
		t.Fatalf("discoveries = (%#v, %v)", discoveries, err)
	}
	firstSeenTotal := 0
	githubSearchFirstSeen := 0
	for _, source := range discoveries.FirstSeenSources {
		firstSeenTotal += source.RepositoryCount
		if source.Source == "github_search" {
			githubSearchFirstSeen = source.RepositoryCount
		}
	}
	if firstSeenTotal != 2 || githubSearchFirstSeen != 2 {
		t.Fatalf("first-seen sources = %#v", discoveries.FirstSeenSources)
	}
	runs, err := adapter.ListJobRuns(ctx, 50, 0)
	if err != nil || len(runs.Items) != 1 {
		t.Fatalf("runs = (%#v, %v)", runs, err)
	}
	if strings.Contains(runs.Items[0].ErrorSummary, "/var/") || len(runs.Items[0].FailureRepositories) != 1 || runs.Items[0].FailureRepositories[0] != "owner/peer" {
		t.Fatalf("run leaked internal target: %#v", runs.Items[0])
	}
	if err := adapter.Ready(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWebAdapterRendersEmptyCatalogBeforeFirstSnapshot(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/empty.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	adapter := WebAdapter{Store: store}

	projects, err := adapter.ListRepositoryTrends(ctx, web.RepositoryQuery{WindowDays: 7, Limit: 50})
	if err != nil {
		t.Fatalf("empty project catalog: %v", err)
	}
	if projects.Total != 0 || len(projects.Items) != 0 || projects.Coverage.AsOfDate.IsZero() {
		t.Fatalf("empty project catalog = %+v", projects)
	}
	topics, err := adapter.ListTopicMetrics(ctx, time.Time{})
	if err != nil {
		t.Fatalf("empty topic catalog: %v", err)
	}
	if len(topics.Items) != 0 || topics.AsOf.IsZero() {
		t.Fatalf("empty topic catalog = %+v", topics)
	}
}

func structRepositoryQuery(asOf time.Time) web.RepositoryQuery {
	return web.RepositoryQuery{AsOf: asOf, Limit: 50}
}

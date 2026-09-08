package app

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebFeedPreservesSavedAnalysisAndItsSource(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/feed.db", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for id := int64(1); id <= 2; id++ {
		if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: id, FullName: fmt.Sprintf("owner/project-%d", id), Source: domain.DiscoverySourceGitHubTrending, DiscoveredAt: now}); err != nil {
			t.Fatal(err)
		}
	}
	analysis, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "研究项目长期变化。", Source: "codex", Model: "fixture-analysis-model", AnalyzedAt: now.Add(-time.Hour), KeyPoints: []string{"保留历史证据"}, UseCases: []string{"日常阅读"}, TechnicalNotes: "来自已保存记录"})
	if err != nil {
		t.Fatal(err)
	}
	adapter := WebAdapter{Store: store}
	page, err := adapter.ListRepositoryTrends(ctx, web.RepositoryQuery{AsOf: now, WindowDays: 7, Limit: 50})
	if err != nil || len(page.Items) != 2 {
		t.Fatalf("feed = %+v, %v", page, err)
	}
	var analyzed web.RepositoryMetric
	for _, item := range page.Items {
		if item.ID == 1 {
			analyzed = item
		} else if item.Analysis != nil {
			t.Fatal("feed fabricated an analysis for an unanalyzed repository")
		}
	}
	got := analyzed.Analysis
	if got == nil || got.SummaryZH != "研究项目长期变化。" || got.Source != "codex" || got.Model != "fixture-analysis-model" || got.Revision != 1 || !got.AnalyzedAt.Equal(now.Add(-time.Hour)) || !reflect.DeepEqual(got.KeyPoints, []string{"保留历史证据"}) || !reflect.DeepEqual(got.UseCases, []string{"日常阅读"}) || got.TechnicalNotes != "来自已保存记录" {
		t.Fatalf("analysis fields or provenance lost: %+v", got)
	}
	detail, err := adapter.GetRepositoryDetail(ctx, 1, now)
	if err != nil || !reflect.DeepEqual(got, detail.Analysis) || !reflect.DeepEqual(got, detail.Repository.Analysis) {
		t.Fatalf("feed/detail analysis mismatch: %+v, %v", detail, err)
	}
	metric := mapRepositoryMetric(domain.RepositoryMetric{Repository: domain.Repository{GitHubRepoID: 1}, Analysis: &analysis})
	if !reflect.DeepEqual(got, metric.Analysis) {
		t.Fatal("plain metric mapping dropped saved analysis")
	}
	metric.Analysis.KeyPoints[0] = "changed client copy"
	if analysis.KeyPoints[0] != "保留历史证据" {
		t.Fatal("web mapping exposed mutable domain analysis arrays")
	}
}

package sqlite

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

func batchProject(name, summary string) domain.AnalysisImportProject {
	return domain.AnalysisImportProject{FullName: name, SummaryZH: summary, Source: "legacy_research", Model: "fixture-model", KeyPoints: []string{"能力"}, UseCases: []string{"场景"}}
}

func TestAnalysisBatchFillsEmptySummariesAndPreservesExistingRecords(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		addRepository(t, store, id, fmt.Sprintf("owner/repo-%d", id))
	}
	before, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 2, SummaryZH: "用户原有摘要", Source: "manual_note", Model: "original", TechnicalNotes: "保留备注", AnalyzedAt: testNow.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	created := storedTime(testNow.Add(-2 * time.Hour))
	if _, err := store.db.Exec(`INSERT INTO repository_analyses (repository_id, summary_zh, source, revision, analyzed_at, created_at, updated_at) VALUES (3, ?, 'manual_note', 4, ?, ?, ?)`, " \n\t ", created, created, created); err != nil {
		t.Fatal(err)
	}
	projects := []domain.AnalysisImportProject{
		batchProject("OWNER/repo-1", " 第一份摘要 "), batchProject("owner/repo-2", "不应覆盖"), batchProject("owner/repo-3", "补齐空摘要"),
	}
	projects[0].RepositoryID = pointer(int64(1))
	projects[0].Model = " fixture-model "
	result, err := store.ImportRepositoryAnalyses(ctx, projects)
	if err != nil || result != (domain.AnalysisBatchResult{Imported: 2, Skipped: 1}) {
		t.Fatalf("batch result = %+v, %v", result, err)
	}
	first, err := store.getRepositoryAnalysis(ctx, 1)
	if err != nil || first == nil || first.SummaryZH != "第一份摘要" || first.Model != "fixture-model" || first.Revision != 1 || !first.AnalyzedAt.Equal(testNow) || !reflect.DeepEqual(first.KeyPoints, []string{"能力"}) {
		t.Fatalf("imported analysis = %+v, %v", first, err)
	}
	after, err := store.getRepositoryAnalysis(ctx, 2)
	if err != nil || after == nil || !reflect.DeepEqual(before, *after) {
		t.Fatalf("nonempty user record changed: before=%+v after=%+v err=%v", before, after, err)
	}
	filled, err := store.getRepositoryAnalysis(ctx, 3)
	if err != nil || filled == nil || filled.Revision != 5 || storedTime(filled.CreatedAt) != created || filled.SummaryZH != "补齐空摘要" {
		t.Fatalf("empty record was not filled correctly: %+v, %v", filled, err)
	}
	result, err = store.ImportRepositoryAnalyses(ctx, projects)
	if err != nil || result != (domain.AnalysisBatchResult{Skipped: 3}) {
		t.Fatalf("repeat import = %+v, %v", result, err)
	}
	repeated, err := store.getRepositoryAnalysis(ctx, 3)
	if err != nil || !reflect.DeepEqual(filled, repeated) {
		t.Fatal("repeat import changed a saved revision or timestamp")
	}
}

func TestAnalysisBatchRollsBackEarlierWritesWhenAnyLaterItemIsInvalid(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*domain.AnalysisImportProject)
	}{
		{"empty-name", func(p *domain.AnalysisImportProject) { p.FullName = " " }},
		{"missing-repository", func(p *domain.AnalysisImportProject) { p.FullName = "owner/not-in-registry" }},
		{"identity-mismatch", func(p *domain.AnalysisImportProject) { p.RepositoryID = pointer(int64(1)) }},
		{"duplicate-case-insensitive-name", func(p *domain.AnalysisImportProject) { p.FullName = "OWNER/repo-1" }},
		{"empty-summary", func(p *domain.AnalysisImportProject) { p.SummaryZH = " \n\t" }},
		{"empty-source", func(p *domain.AnalysisImportProject) { p.Source = " " }},
		{"invalid-time", func(p *domain.AnalysisImportProject) { p.AnalyzedAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, _ := newTestStore(t)
			addRepository(t, store, 1, "owner/repo-1")
			addRepository(t, store, 2, "owner/repo-2")
			projects := []domain.AnalysisImportProject{batchProject("owner/repo-1", "先写入的摘要"), batchProject("owner/repo-2", "第二份摘要")}
			test.change(&projects[1])
			result, err := store.ImportRepositoryAnalyses(context.Background(), projects)
			if err == nil || result != (domain.AnalysisBatchResult{}) {
				t.Fatalf("invalid batch reported success/partial writes: %+v, %v", result, err)
			}
			var count int
			if err := store.db.QueryRow(`SELECT COUNT(*) FROM repository_analyses`).Scan(&count); err != nil || count != 0 {
				t.Fatalf("rollback left %d analyses, error=%v", count, err)
			}
		})
	}
}

func TestAnalysisBatchChecksSkippedItemsAndRollsBackDatabaseFailures(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/repo-1")
	addRepository(t, store, 2, "owner/repo-2")
	if _, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 2, SummaryZH: "已保存", Source: "manual"}); err != nil {
		t.Fatal(err)
	}
	invalid := batchProject("owner/repo-2", "")
	if _, err := store.ImportRepositoryAnalyses(ctx, []domain.AnalysisImportProject{batchProject("owner/repo-1", "新摘要"), invalid}); err == nil {
		t.Fatal("invalid skipped entry was silently accepted")
	}
	if first, err := store.getRepositoryAnalysis(ctx, 1); err != nil || first != nil {
		t.Fatal("skipped-entry validation did not roll back earlier writes")
	}
	if _, err := store.db.Exec(`DELETE FROM repository_analyses WHERE repository_id=2`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_batch_fixture BEFORE INSERT ON repository_analyses WHEN NEW.repository_id=2 BEGIN SELECT RAISE(ABORT, 'fixture insert failure'); END`); err != nil {
		t.Fatal(err)
	}
	result, err := store.ImportRepositoryAnalyses(ctx, []domain.AnalysisImportProject{batchProject("owner/repo-1", "新摘要"), batchProject("owner/repo-2", "另一份摘要")})
	if err == nil || result != (domain.AnalysisBatchResult{}) {
		t.Fatalf("database failure result = %+v, %v", result, err)
	}
	if first, err := store.getRepositoryAnalysis(ctx, 1); err != nil || first != nil {
		t.Fatal("database failure left an earlier import committed")
	}
}

func TestAnalysisBatchHandlesResearchVolumeAndRejectsCountOutsideBounds(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for _, input := range [][]domain.AnalysisImportProject{nil, make([]domain.AnalysisImportProject, 5001)} {
		if _, err := store.ImportRepositoryAnalyses(ctx, input); err == nil {
			t.Fatal("invalid batch count accepted")
		}
	}
	const count = 1186
	if _, err := store.db.Exec(`WITH RECURSIVE ids(id) AS (SELECT 1 UNION ALL SELECT id+1 FROM ids WHERE id<1186)
INSERT INTO repositories (github_repo_id, full_name, first_seen_at, first_seen_source, last_discovered_at, created_at, updated_at)
SELECT id, 'research/project-' || id, '2026-08-01T00:00:00Z', 'legacy', '2026-08-30T00:00:00Z', '2026-08-01T00:00:00Z', '2026-08-30T00:00:00Z' FROM ids`); err != nil {
		t.Fatal(err)
	}
	projects := make([]domain.AnalysisImportProject, count)
	for index := range projects {
		projects[index] = batchProject(fmt.Sprintf("research/project-%d", index+1), fmt.Sprintf("第%d份研究摘要", index+1))
	}
	result, err := store.ImportRepositoryAnalyses(ctx, projects)
	if err != nil || result.Imported != count || result.Skipped != 0 {
		t.Fatalf("research-volume import = %+v, %v", result, err)
	}
	var stored int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM repository_analyses`).Scan(&stored); err != nil || stored != count {
		t.Fatalf("stored %d analyses, error=%v", stored, err)
	}
}

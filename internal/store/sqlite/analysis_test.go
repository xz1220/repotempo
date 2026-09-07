package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/migrations"
)

func TestPutRepositoryAnalysisCreatesAndRevisesStoredInterpretation(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 31, "owner/analyzed-through-api")

	first, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{
		RepositoryID: 31,
		SummaryZH:    " 第一版摘要。 ",
		Source:       "codex",
		Model:        "gpt-test",
	})
	if err != nil {
		t.Fatalf("create repository analysis: %v", err)
	}
	if first.Revision != 1 || first.SummaryZH != "第一版摘要。" || first.Source != "codex" ||
		len(first.KeyPoints) != 0 || len(first.UseCases) != 0 {
		t.Fatalf("first repository analysis = %+v", first)
	}

	store.now = func() time.Time { return testNow.Add(time.Minute) }
	second, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{
		RepositoryID:   31,
		SummaryZH:      "第二版摘要。",
		KeyPoints:      []string{"能力一"},
		UseCases:       []string{"场景一"},
		TechnicalNotes: " 技术备注 ",
		Source:         "codex",
		Model:          "gpt-test-2",
		AnalyzedAt:     testNow.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("revise repository analysis: %v", err)
	}
	if second.Revision != 2 || second.SummaryZH != "第二版摘要。" ||
		!reflect.DeepEqual(second.KeyPoints, []string{"能力一"}) ||
		!reflect.DeepEqual(second.UseCases, []string{"场景一"}) ||
		second.TechnicalNotes != "技术备注" || second.Model != "gpt-test-2" {
		t.Fatalf("second repository analysis = %+v", second)
	}
	if !second.CreatedAt.Equal(first.CreatedAt) || !second.UpdatedAt.After(first.UpdatedAt) {
		t.Fatalf("repository analysis timestamps: first=%+v second=%+v", first, second)
	}
}

func TestRepositoryAnalysisMigrationSafelyImportsManualNotes(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "version-two.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open version-two database: %v", err)
	}
	for _, name := range []string{"0001_initial.sql", "0002_trend_indexes.sql"} {
		script, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if _, err := database.ExecContext(ctx, string(script)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	if _, err := database.ExecContext(ctx, "PRAGMA user_version = 2"); err != nil {
		t.Fatalf("set version-two marker: %v", err)
	}

	createdAt := testNow.Add(-2 * time.Hour)
	updatedAt := testNow.Add(-time.Hour)
	for _, repository := range []struct {
		id   int64
		name string
		note string
	}{
		{id: 1, name: "owner/existing", note: "不应覆盖已有解读"},
		{id: 2, name: "owner/imported", note: "保留已有手工备注"},
		{id: 3, name: "owner/empty", note: ""},
		{id: 4, name: "owner/whitespace", note: " \n\t "},
	} {
		_, err := database.ExecContext(ctx, `
INSERT INTO repositories (
    github_repo_id, full_name, first_seen_at, first_seen_source,
    discovery_sources_json, last_discovered_at, manual_note, created_at, updated_at
)
VALUES (?, ?, ?, 'manual', '["manual"]', ?, ?, ?, ?)`,
			repository.id,
			repository.name,
			storedTime(createdAt),
			storedTime(createdAt),
			repository.note,
			storedTime(createdAt),
			storedTime(updatedAt),
		)
		if err != nil {
			t.Fatalf("insert repository %d: %v", repository.id, err)
		}
	}

	// Apply the migration once without advancing user_version, then customize
	// one seeded row. OpenWithConfig will execute the complete SQL a second time
	// and must neither fail nor overwrite that existing analysis.
	migration, err := fs.ReadFile(migrations.Files, "0003_repository_analyses.sql")
	if err != nil {
		t.Fatalf("read analysis migration: %v", err)
	}
	if _, err := database.ExecContext(ctx, string(migration)); err != nil {
		t.Fatalf("apply analysis migration before version advance: %v", err)
	}
	if _, err := database.ExecContext(ctx, `
UPDATE repository_analyses SET
    summary_zh = '已有结构化解读',
    key_points_json = '["已有要点"]',
    use_cases_json = '["已有场景"]',
    technical_notes = '已有技术备注',
    source = 'manual',
    revision = 7,
    analyzed_at = ?,
    created_at = ?,
    updated_at = ?
WHERE repository_id = 1`,
		storedTime(updatedAt), storedTime(createdAt), storedTime(updatedAt)); err != nil {
		t.Fatalf("insert preexisting analysis: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close version-two database: %v", err)
	}

	store, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatalf("migrate version-two database: %v", err)
	}
	defer func() { _ = store.Close() }()

	var version int
	if err := store.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil || version != 4 {
		t.Fatalf("migration version = %d, err = %v, want 4", version, err)
	}
	var analysisCount int
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_analyses").Scan(&analysisCount); err != nil {
		t.Fatalf("count repository analyses: %v", err)
	}
	if analysisCount != 2 {
		t.Fatalf("analysis count = %d, want existing plus one imported", analysisCount)
	}

	existing, err := store.getRepositoryAnalysis(ctx, 1)
	if err != nil {
		t.Fatalf("read existing analysis: %v", err)
	}
	if existing == nil || existing.SummaryZH != "已有结构化解读" || existing.Source != "manual" || existing.Revision != 7 {
		t.Fatalf("preexisting analysis was overwritten: %+v", existing)
	}
	imported, err := store.getRepositoryAnalysis(ctx, 2)
	if err != nil {
		t.Fatalf("read imported analysis: %v", err)
	}
	if imported == nil || imported.SummaryZH != "保留已有手工备注" || imported.Source != "imported" ||
		imported.Model != "" || imported.Revision != 1 || len(imported.KeyPoints) != 0 || len(imported.UseCases) != 0 {
		t.Fatalf("imported analysis = %+v", imported)
	}
	if !imported.AnalyzedAt.Equal(updatedAt) || !imported.CreatedAt.Equal(createdAt) || !imported.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("imported timestamps = %+v", imported)
	}
	for _, repositoryID := range []int64{3, 4} {
		analysis, err := store.getRepositoryAnalysis(ctx, repositoryID)
		if err != nil || analysis != nil {
			t.Fatalf("analysis for blank note repository %d = (%+v, %v), want nil", repositoryID, analysis, err)
		}
	}
	repository, err := store.GetRepository(ctx, 2)
	if err != nil || repository.ManualNote != "保留已有手工备注" {
		t.Fatalf("manual note changed during import: repository=%+v err=%v", repository, err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close migrated store: %v", err)
	}
	reopened, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatalf("reopen migrated store: %v", err)
	}
	store = reopened
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repository_analyses").Scan(&analysisCount); err != nil || analysisCount != 2 {
		t.Fatalf("analysis count after reopen = %d, err = %v", analysisCount, err)
	}
}

func TestRepositoryDetailLoadsOptionalAnalysis(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 11, "owner/analyzed")
	addRepository(t, store, 12, "owner/plain")
	analyzedAt := testNow.Add(-30 * time.Minute)
	createdAt := testNow.Add(-20 * time.Minute)
	updatedAt := testNow.Add(-10 * time.Minute)
	_, err := store.db.ExecContext(ctx, `
INSERT INTO repository_analyses (
    repository_id, summary_zh, key_points_json, use_cases_json,
    technical_notes, source, model, revision, analyzed_at, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		11,
		"这是中文项目摘要。",
		`["单一二进制部署","保留来源信息"]`,
		`["技术趋势研究","开源项目监控"]`,
		"Go 与 SQLite；无远程前端依赖。",
		"generated",
		"analysis-model-v1",
		3,
		storedTime(analyzedAt),
		storedTime(createdAt),
		storedTime(updatedAt),
	)
	if err != nil {
		t.Fatalf("insert repository analysis: %v", err)
	}

	detail, err := store.GetRepositoryDetail(ctx, 11, date("2026-08-30"))
	if err != nil {
		t.Fatalf("get analyzed repository detail: %v", err)
	}
	if detail.Analysis == nil {
		t.Fatal("repository detail analysis is nil")
	}
	analysis := detail.Analysis
	if analysis.RepositoryID != 11 || analysis.SummaryZH != "这是中文项目摘要。" ||
		!reflect.DeepEqual(analysis.KeyPoints, []string{"单一二进制部署", "保留来源信息"}) ||
		!reflect.DeepEqual(analysis.UseCases, []string{"技术趋势研究", "开源项目监控"}) ||
		analysis.TechnicalNotes != "Go 与 SQLite；无远程前端依赖。" ||
		analysis.Source != "generated" || analysis.Model != "analysis-model-v1" || analysis.Revision != 3 {
		t.Fatalf("repository analysis = %+v", analysis)
	}
	if !analysis.AnalyzedAt.Equal(analyzedAt) || !analysis.CreatedAt.Equal(createdAt) || !analysis.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("repository analysis timestamps = %+v", analysis)
	}

	plain, err := store.GetRepositoryDetail(ctx, 12, date("2026-08-30"))
	if err != nil {
		t.Fatalf("get plain repository detail: %v", err)
	}
	if plain.Analysis != nil {
		t.Fatalf("plain repository analysis = %+v, want nil", plain.Analysis)
	}
}

func TestRepositoryDetailNormalizesDefaultDateOnce(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 13, "owner/midnight")
	putSuccess(t, store, 13, "2026-08-30", 100)
	putSuccess(t, store, 13, "2026-08-31", 200)

	callCount := 0
	store.now = func() time.Time {
		callCount++
		if callCount == 1 {
			return time.Date(2026, 8, 30, 15, 59, 59, 0, time.UTC)
		}
		return time.Date(2026, 8, 30, 16, 0, 1, 0, time.UTC)
	}
	detail, err := store.GetRepositoryDetail(ctx, 13, "")
	if err != nil {
		t.Fatalf("get repository detail across Shanghai midnight: %v", err)
	}
	if callCount != 1 {
		t.Fatalf("store clock call count = %d, want 1", callCount)
	}
	if detail.Metric.Growth.Current == nil || *detail.Metric.Growth.Current != 100 {
		t.Fatalf("detail current stars = %+v, want 100", detail.Metric.Growth.Current)
	}
	if len(detail.History) != 1 || detail.History[0].SnapshotDate != date("2026-08-30") {
		t.Fatalf("detail history = %+v, want only 2026-08-30", detail.History)
	}
}

func TestRepositoryAnalysisConstraints(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 21, "owner/constrained")
	baseArguments := []any{
		21,
		"摘要",
		`["要点"]`,
		`["场景"]`,
		"",
		"generated",
		"model",
		1,
		storedTime(testNow),
		storedTime(testNow),
		storedTime(testNow),
	}
	insert := `
INSERT INTO repository_analyses (
    repository_id, summary_zh, key_points_json, use_cases_json,
    technical_notes, source, model, revision, analyzed_at, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	invalidJSON := append([]any(nil), baseArguments...)
	invalidJSON[2] = `{"not":"an array"}`
	if _, err := store.db.ExecContext(ctx, insert, invalidJSON...); err == nil {
		t.Fatal("analysis table accepted an object for key_points_json")
	}
	invalidKeyPointElement := append([]any(nil), baseArguments...)
	invalidKeyPointElement[2] = `[1]`
	if _, err := store.db.ExecContext(ctx, insert, invalidKeyPointElement...); err == nil {
		t.Fatal("analysis table accepted a non-string key point")
	}
	invalidUseCaseElement := append([]any(nil), baseArguments...)
	invalidUseCaseElement[3] = `[null]`
	if _, err := store.db.ExecContext(ctx, insert, invalidUseCaseElement...); err == nil {
		t.Fatal("analysis table accepted a non-string use case")
	}
	invalidRevision := append([]any(nil), baseArguments...)
	invalidRevision[7] = 0
	if _, err := store.db.ExecContext(ctx, insert, invalidRevision...); err == nil {
		t.Fatal("analysis table accepted revision zero")
	}
	if _, err := store.db.ExecContext(ctx, insert, baseArguments...); err != nil {
		t.Fatalf("insert valid constrained analysis: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"UPDATE repository_analyses SET use_cases_json = '[1]' WHERE repository_id = 21",
	); err == nil {
		t.Fatal("analysis table accepted a non-string use case during update")
	}
	if _, err := store.db.ExecContext(ctx, "DELETE FROM repositories WHERE github_repo_id = 21"); err != nil {
		t.Fatalf("delete analyzed repository: %v", err)
	}
	var count int
	if err := store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM repository_analyses WHERE repository_id = 21",
	).Scan(&count); err != nil || count != 0 {
		t.Fatalf("analysis count after repository delete = %d, err = %v, want 0", count, err)
	}
}

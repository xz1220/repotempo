package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/migrations"
)

func newV3MigrationFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "v3.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, name := range []string{"0001_initial.sql", "0002_trend_indexes.sql", "0003_repository_analyses.sql"} {
		script, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version = 3; PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO repositories (` + repositoryColumns + `)
		VALUES (42, 'node-42', 'owner/existing', 'https://github.com/owner/existing', '原始项目描述',
		'Go', '2025-01-02T03:04:05Z', '2026-08-11T00:05:06.123456789Z', 'legacy',
		'original-profile', '["legacy","manual","github_search"]', '2026-09-06T01:02:03Z',
		'paused', 'archived', 1, '原始备注', 'W/"original-etag"', '2026-09-06T02:03:04Z',
		'["old-owner/existing"]', '2026-08-11T00:00:00Z', '2026-09-06T02:03:04.123456Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO topics (id, slug, name, status, created_at, updated_at)
		VALUES (1, 'coding-agents', '编程助手', 'active', '2026-08-11T00:00:00Z', '2026-09-06T00:00:00Z');
		INSERT INTO repository_topics (repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at)
		VALUES (42, 1, 'manual', 1, 0.9, '2026-08-11T01:02:03Z', '2026-09-06T02:03:04Z');
		INSERT INTO daily_snapshots (` + snapshotColumns + `)
		VALUES (42, '2026-09-05', '2026-09-05T01:02:03.456Z', 12345, 'success', 200, '', 3, 555, 0.25, '2026-09-05T02:03:04Z'),
		       (42, '2026-09-06', '2026-09-06T01:02:03Z', NULL, 'failed', 503, 'upstream-error', NULL, NULL, NULL, '2026-09-06T02:03:04Z');
		INSERT INTO repository_analyses (repository_id, summary_zh, key_points_json, use_cases_json, technical_notes, source, model, revision, analyzed_at, created_at, updated_at)
		VALUES (42, '保留研究解读', '["能力一","能力二"]', '["场景"]', '技术说明', 'codex', 'fixture-model', 7, '2026-09-06T02:03:04Z', '2026-08-11T01:02:03Z', '2026-09-06T02:03:04Z');
		INSERT INTO job_runs (run_id, job_type, started_at, finished_at, status, target_count, success_count, failure_count, skipped_count, details_json, error_summary, created_at)
		VALUES ('original-run', 'snapshot', '2026-09-06T01:00:00Z', '2026-09-06T02:00:00Z', 'partial', 2, 1, 1, 0, '{"date":"2026-09-06","note":"保留"}', 'one fetch failed', '2026-09-06T01:00:00Z');
		CREATE TABLE migration_audit (message TEXT NOT NULL);
		CREATE INDEX custom_repository_description ON repositories(lower(description)) WHERE is_focus = 1;
		CREATE TRIGGER custom_repository_insert AFTER INSERT ON repositories BEGIN
		  INSERT INTO migration_audit(message) VALUES ('insert:' || NEW.github_repo_id);
		END;
		CREATE TRIGGER custom_snapshot_update AFTER UPDATE ON daily_snapshots BEGIN
		  INSERT INTO migration_audit(message) SELECT full_name FROM repositories WHERE github_repo_id = NEW.repository_id;
		END;
		CREATE VIEW existing_repository_view AS SELECT github_repo_id, full_name, manual_note FROM repositories;
	`); err != nil {
		t.Fatal(err)
	}
	// Production has old orphan mappings. The rebuild must neither reject nor
	// silently clean these 161 historical rows.
	if _, err := db.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 161; index++ {
		if _, err := db.Exec(`INSERT INTO repository_topics (repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at)
			VALUES (?, 1, 'imported', 0, NULL, '2026-08-11T01:02:03Z', '2026-08-11T01:02:03Z')`, 10000+index); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func migrationRows(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	result := make([][]any, 0)
	for rows.Next() {
		values, pointers := make([]any, len(columns)), make([]any, len(columns))
		for index := range values {
			pointers[index] = &values[index]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func migrationEvidence(t *testing.T, db *sql.DB) map[string][][]any {
	t.Helper()
	result := map[string][][]any{}
	for _, table := range []string{"repositories", "daily_snapshots", "repository_topics", "repository_analyses", "topics", "job_runs", "migration_audit"} {
		result[table] = migrationRows(t, db, "SELECT rowid, * FROM "+table+" ORDER BY rowid")
	}
	result["dependent_schema"] = migrationRows(t, db, `SELECT type, name, tbl_name, sql FROM sqlite_schema
		WHERE name NOT LIKE 'sqlite_%' AND name != 'repositories' ORDER BY type, name`)
	result["view_results"] = migrationRows(t, db, "SELECT * FROM existing_repository_view")
	result["foreign_key_check"] = migrationRows(t, db, "SELECT * FROM pragma_foreign_key_check ORDER BY \"table\", rowid, parent, fkid")
	return result
}

func assertMigrationSettings(t *testing.T, db *sql.DB, wantVersion, wantLegacy int) {
	t.Helper()
	for name, want := range map[string]int{"user_version": wantVersion, "foreign_keys": 1, "legacy_alter_table": wantLegacy} {
		var actual int
		if err := db.QueryRow("PRAGMA " + name).Scan(&actual); err != nil || actual != want {
			t.Fatalf("%s = %d, error %v, want %d", name, actual, err, want)
		}
	}
	var temporaryTables int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name = 'repositories_v4'").Scan(&temporaryTables); err != nil || temporaryTables != 0 {
		t.Fatalf("temporary migration table survived: %d, %v", temporaryTables, err)
	}
}

func TestRepositorySourceMigrationPreservesAllV3EvidenceAndLegacyOrphans(t *testing.T) {
	db, path := newV3MigrationFixture(t)
	before := migrationEvidence(t, db)
	if len(before["foreign_key_check"]) != 161 {
		t.Fatal("fixture must include 161 historical orphans")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assertMigrationSettings(t, store.db, 4, 0)
	after := migrationEvidence(t, store.db)
	if !reflect.DeepEqual(before, after) {
		for name, original := range before {
			if !reflect.DeepEqual(original, after[name]) {
				t.Errorf("migration changed %s: before=%#v after=%#v", name, original, after[name])
			}
		}
	}
	if err := applyMigrations(context.Background(), store.db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after, migrationEvidence(t, store.db)) {
		t.Fatal("repeated migration changed existing evidence")
	}
	added, created, err := store.UpsertRepository(context.Background(), domain.RepositoryObservation{GitHubRepoID: 43, FullName: "owner/trending", Source: domain.DiscoverySourceGitHubTrending, Profile: "daily", DiscoveredAt: testNow})
	if err != nil || !created || added.FirstSeenSource != domain.DiscoverySourceGitHubTrending {
		t.Fatalf("new source rejected: %#v, %t, %v", added, created, err)
	}
	if _, err := store.db.Exec(`UPDATE repositories SET first_seen_source = 'invented_source' WHERE github_repo_id = 43`); err == nil {
		t.Fatal("unknown discovery source bypassed the new CHECK")
	}
	if rows := migrationRows(t, store.db, "SELECT message FROM migration_audit"); !reflect.DeepEqual(rows, [][]any{{"insert:43"}}) {
		t.Fatalf("restored repository trigger did not run: %#v", rows)
	}
	if _, err := store.db.Exec("INSERT INTO repository_topics(repository_id, topic_id, source, confirmed, assigned_at, updated_at) VALUES (99999, 1, 'auto', 0, '2026-09-07T00:00:00Z', '2026-09-07T00:00:00Z')"); err == nil {
		t.Fatal("foreign keys remained disabled after migration")
	}
	// FK definitions must still point to repositories and retain real cascades.
	if _, err := store.db.Exec("DELETE FROM repositories WHERE github_repo_id = 42"); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"daily_snapshots", "repository_topics", "repository_analyses"} {
		var remaining int
		if err := store.db.QueryRow("SELECT COUNT(*) FROM " + table + " WHERE repository_id = 42").Scan(&remaining); err != nil || remaining != 0 {
			t.Fatalf("%s lost its cascade: count %d, %v", table, remaining, err)
		}
	}
}

func TestRepositorySourceMigrationRollsBackAndRestoresConnectionSettings(t *testing.T) {
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, appended string }{
		{"SQL failure after rebuild", "; INSERT INTO nonexistent_migration_target VALUES (1);"},
		{"new foreign key violation", `; INSERT INTO repository_topics (repository_id,topic_id,source,confirmed,assigned_at,updated_at) VALUES (99999,1,'auto',0,'2026-09-07T00:00:00Z','2026-09-07T00:00:00Z');`},
		{"removed historical orphan", "; DELETE FROM repository_topics WHERE repository_id = 10000;"},
	} {
		t.Run(test.name, func(t *testing.T) {
			db, _ := newV3MigrationFixture(t)
			before := migrationEvidence(t, db)
			originalSchema := migrationRows(t, db, "SELECT sql FROM sqlite_schema WHERE name='repositories'")
			if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)+test.appended); err == nil {
				t.Fatal("broken migration unexpectedly committed")
			}
			assertMigrationSettings(t, db, 3, 0)
			if !reflect.DeepEqual(before, migrationEvidence(t, db)) || !reflect.DeepEqual(originalSchema, migrationRows(t, db, "SELECT sql FROM sqlite_schema WHERE name='repositories'")) {
				t.Fatal("failed migration did not roll back schema and every stored value")
			}
			if err := applyMigrations(context.Background(), db); err != nil {
				t.Fatalf("retry after rollback failed: %v", err)
			}
			assertMigrationSettings(t, db, 4, 0)
		})
	}
}

func TestRepositorySourceMigrationPreservesExistingLegacyAlterSetting(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	if _, err := db.Exec("PRAGMA legacy_alter_table = ON"); err != nil {
		t.Fatal(err)
	}
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertMigrationSettings(t, db, 4, 1)
}

func TestRepositorySourceMigrationIndexRestorationFailureRollsBack(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	before := migrationEvidence(t, db)
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	// Simulate a defective replacement schema which cannot support an existing
	// expression index. Its failure must roll back the already executed rebuild.
	if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)+"; ALTER TABLE repositories DROP COLUMN description;"); err == nil || !strings.Contains(err.Error(), "restore repository index or trigger") {
		t.Fatalf("expected index-restoration failure, got %v", err)
	}
	assertMigrationSettings(t, db, 3, 0)
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
		t.Fatal("index-restoration failure did not restore the original schema and data")
	}
}

func TestRepositorySourceMigrationRefusesToDiscardCustomColumns(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	if _, err := db.Exec("ALTER TABLE repositories ADD COLUMN operator_metadata TEXT; UPDATE repositories SET operator_metadata = 'preserve extra metadata'"); err != nil {
		t.Fatal(err)
	}
	before := migrationEvidence(t, db)
	if err := applyMigrations(context.Background(), db); err == nil || !strings.Contains(err.Error(), "refusing to discard") {
		t.Fatalf("custom metadata was not protected: %v", err)
	}
	assertMigrationSettings(t, db, 3, 0)
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
		t.Fatal("custom metadata changed")
	}
}

func TestRepositorySourceMigrationCancellationRollsBack(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	before := migrationEvidence(t, db)
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err = applyRepositoryRebuildMigration(ctx, db, 4, string(script)+`
		; WITH RECURSIVE work(n) AS (SELECT 1 UNION ALL SELECT n + 1 FROM work WHERE n < 100000000)
		SELECT sum(n) FROM work;`)
	if err == nil || ctx.Err() == nil {
		t.Fatalf("expected interrupted migration, got %v, context %v", err, ctx.Err())
	}
	assertMigrationSettings(t, db, 3, 0)
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
		t.Fatal("canceled migration changed stored data")
	}
}

func TestRepositorySourceMigrationFailureDuringStartupLeavesV3Untouched(t *testing.T) {
	db, path := newV3MigrationFixture(t)
	if _, err := db.Exec("CREATE TABLE repositories_v4 (operator_owned_value TEXT); INSERT INTO repositories_v4 VALUES ('preserve me')"); err != nil {
		t.Fatal(err)
	}
	before := migrationEvidence(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := Open(context.Background(), path)
	if err == nil {
		store.Close()
		t.Fatal("colliding replacement-table name did not stop migration")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected startup failure: %v", err)
	}
	reopened, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if !reflect.DeepEqual(before, migrationEvidence(t, reopened)) {
		t.Fatal("failed startup changed v3 data")
	}
	var version int
	if err := reopened.QueryRow("PRAGMA user_version").Scan(&version); err != nil || version != 3 {
		t.Fatalf("failed startup changed version: %d, %v", version, err)
	}
	if rows := migrationRows(t, reopened, "SELECT * FROM repositories_v4"); !reflect.DeepEqual(rows, [][]any{{"preserve me"}}) {
		t.Fatalf("existing table was overwritten: %s", fmt.Sprint(rows))
	}
}

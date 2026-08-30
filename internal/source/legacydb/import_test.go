package legacydb

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestImportRealLegacySchema(t *testing.T) {
	database, err := sql.Open("sqlite", "file:legacy-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	defer database.Close()
	schema := []string{
		`CREATE TABLE repos (
          repo_id INTEGER PRIMARY KEY, repo_name TEXT, primary_language TEXT,
          description TEXT, github_stars INTEGER, github_topics TEXT, html_url TEXT,
          github_status TEXT, archived INTEGER, metadata_etag TEXT,
          first_seen_at TEXT, last_seen_at TEXT, is_active INTEGER,
          manual_favorite INTEGER
        )`,
		`CREATE TABLE catalog_projects (repo_id INTEGER, added_on TEXT, added_at TEXT, source TEXT)`,
		`CREATE TABLE daily_observations (
          observation_date TEXT, repo_id INTEGER, observed_at TEXT, rank INTEGER,
          status TEXT, in_trending INTEGER, window_stars INTEGER,
          github_stars INTEGER, PRIMARY KEY(observation_date, repo_id)
        )`,
		`CREATE TABLE trend_snapshots (snapshot_at TEXT, period TEXT, rank INTEGER, repo_id INTEGER, stars INTEGER)`,
		`CREATE TABLE day_snapshots (snapshot_at TEXT, repo_id INTEGER, stars INTEGER)`,
		`INSERT INTO repos VALUES
          (1, 'new/name', 'Go', 'one', 999, '["ai-agent"]', 'https://github.com/new/name', 'active', 0, 'etag-1', '2026-01-01T00:00:00Z', '2026-08-29T00:00:00Z', 1, 0),
          (2, 'other/repo', 'Rust', 'two', 0, '[]', 'https://github.com/other/repo', 'archived', 1, '', '2026-02-01T00:00:00Z', '2026-08-29T00:00:00Z', 0, 1)`,
		`INSERT INTO catalog_projects VALUES (1, '2026-01-02', '2026-01-02T01:00:00Z', 'ossinsight')`,
		`INSERT INTO daily_observations VALUES
          ('2026-08-28', 1, '2026-08-28T01:00:00Z', 1, 'ok', 1, 20, 100),
          ('2026-08-29', 1, '2026-08-29T01:00:00Z', 2, 'ok', 1, 10, NULL),
          ('2026-08-28', 2, '2026-08-28T02:00:00Z', NULL, 'ok', 0, NULL, 0),
          ('2026-08-30', 2, NULL, NULL, 'ok', 0, NULL, 1)`,
		`INSERT INTO trend_snapshots VALUES ('2026-08-28T00:00:00Z', 'past_week', 1, 1, 20), ('2026-08-29T00:00:00Z', 'past_week', 1, 1, 10)`,
		`INSERT INTO day_snapshots VALUES ('2026-08-28T00:00:00Z', 1, 100)`,
	}
	for _, statement := range schema {
		if _, err := database.Exec(statement); err != nil {
			t.Fatalf("exec schema: %v\n%s", err, statement)
		}
	}
	result, err := Import(context.Background(), database, Options{Location: time.FixedZone("CST", 8*60*60)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %+v", result.Candidates)
	}
	if !result.Candidates[0].IsFocus || !result.Candidates[1].IsFocus {
		t.Fatalf("catalog/manual focus markers not retained: %+v", result.Candidates)
	}
	if result.Candidates[0].Metadata["etag"] != "etag-1" || result.Candidates[1].Repository.GitHubStatus != "archived" {
		t.Fatalf("legacy GitHub state not retained: %+v", result.Candidates)
	}
	if result.Candidates[1].MonitorStatus != "" || result.Candidates[1].Metadata["legacy_is_active"] != "false" {
		t.Fatalf("legacy trend membership incorrectly became a monitoring pause: %+v", result.Candidates[1])
	}
	for _, candidate := range result.Candidates {
		if candidate.Repository.AbsoluteStars != nil {
			t.Fatalf("undated repos.github_stars imported: %+v", candidate)
		}
	}
	if len(result.Observations) != 2 {
		t.Fatalf("observations = %+v, want two timestamped non-NULL values", result.Observations)
	}
	if result.Observations[1].StarCount != 0 {
		t.Fatalf("real zero observation was not preserved: %+v", result.Observations[1])
	}
	for _, observation := range result.Observations {
		if observation.FixedPanel {
			t.Fatalf("legacy observation incorrectly marked fixed panel: %+v", observation)
		}
	}
	if result.Stats.IgnoredTrendSnapshots != 2 || result.Stats.IgnoredDaySnapshots != 1 || result.Stats.IgnoredRepositoryStars != 2 {
		t.Fatalf("stats = %+v", result.Stats)
	}
	foundMissingObservedAt := false
	for _, warning := range result.Warnings {
		if warning.Code == "missing_observed_at" {
			foundMissingObservedAt = true
		}
	}
	if !foundMissingObservedAt {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
}

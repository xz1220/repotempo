package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
)

// BenchmarkRadar queries a real, temporary SQLite database. Every scenario
// contains 31 days of snapshots plus the previous monthly boundary, four topic
// groups, new discoveries, focus entries, failed and absent observations.
// Run with: go test ./internal/store/sqlite -run '^$' -bench BenchmarkRadar -benchtime=1x -benchmem
func BenchmarkRadar(b *testing.B) {
	for _, count := range []int{4000, 10000} {
		b.Run(fmt.Sprintf("%d_repositories", count), func(b *testing.B) {
			ctx := context.Background()
			store, err := Open(ctx, filepath.Join(b.TempDir(), "radar-benchmark.db"))
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(func() { _ = store.Close() })
			seedRadarBenchmark(b, store, count)
			if os.Getenv("RADAR_EXPLAIN") == "1" {
				logRadarQueryPlan(b, store)
			}
			for _, days := range []int{7, 30} {
				b.Run(fmt.Sprintf("overview_%dd", days), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						value, err := store.RadarOverview(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: days})
						if err != nil || value.Coverage.ScopeCount != count {
							b.Fatalf("overview count %d, error %v", value.Coverage.ScopeCount, err)
						}
					}
				})
			}
			for _, ordering := range []domain.RepositoryTrendSort{domain.RepositoryTrendSortNewest, domain.RepositoryTrendSortSlowdown} {
				b.Run("page_"+string(ordering), func(b *testing.B) {
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						value, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{
							AsOf: date("2026-08-30"), WindowDays: 7, Sort: ordering, Limit: 50,
						})
						if err != nil || len(value.Items) != 50 {
							b.Fatalf("page size %d, error %v", len(value.Items), err)
						}
					}
				})
			}
		})
	}
}

func logRadarQueryPlan(b *testing.B, store *Store) {
	b.Helper()
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7}
	scope, arguments := trendScope(query)
	arguments = append(arguments, query.AsOf, date("2026-08-23"), 7, query.AsOf)
	rows, err := store.db.Query("EXPLAIN QUERY PLAN "+trendBaseCTE(scope)+`
SELECT * FROM scored ORDER BY julianday(first_seen_at) DESC LIMIT 51`, arguments...)
	if err != nil {
		b.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			b.Fatal(err)
		}
		b.Logf("plan %d parent %d: %s", id, parent, detail)
	}
	if err := rows.Err(); err != nil {
		b.Fatal(err)
	}
}

func seedRadarBenchmark(b *testing.B, store *Store, count int) {
	b.Helper()
	ctx := context.Background()
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer transaction.Rollback()
	statements := []struct {
		sql  string
		args []any
	}{
		{`WITH RECURSIVE ids(id) AS (SELECT 1 UNION ALL SELECT id+1 FROM ids WHERE id < ?)
INSERT INTO repositories (github_repo_id, full_name, description, first_seen_at, first_seen_source, last_discovered_at, is_focus, created_at, updated_at)
SELECT id, 'benchmark/repo-' || id, 'AI agent benchmark repository',
       CASE WHEN id % 20 = 0 THEN '2026-08-30T00:00:00Z' ELSE '2026-06-01T00:00:00Z' END,
       'github_search', '2026-08-30T00:00:00Z', CASE WHEN id % 5 = 0 THEN 1 ELSE 0 END,
       '2026-06-01T00:00:00Z', '2026-08-30T00:00:00Z'
FROM ids`, []any{count}},
		{`WITH RECURSIVE days(day) AS (SELECT 0 UNION ALL SELECT day+1 FROM days WHERE day < 30),
dates(day) AS (SELECT day FROM days UNION ALL SELECT 60)
INSERT INTO daily_snapshots (repository_id, snapshot_date, captured_at, star_count, fetch_status, http_status, created_at)
SELECT r.github_repo_id, date('2026-08-30', '-' || dates.day || ' days'), '2026-08-30T00:00:00Z',
       CASE WHEN dates.day = 0 AND r.github_repo_id % 11 = 0 THEN NULL
            ELSE 10000 + r.github_repo_id * 2 - dates.day * (1 + r.github_repo_id % 30)
                 + CASE WHEN dates.day >= 7 THEN 10 + r.github_repo_id % 50 ELSE 0 END END,
       CASE WHEN dates.day = 0 AND r.github_repo_id % 11 = 0 THEN 'failed' ELSE 'success' END,
       CASE WHEN dates.day = 0 AND r.github_repo_id % 11 = 0 THEN 503 ELSE 200 END,
       '2026-08-30T00:00:00Z'
FROM repositories r CROSS JOIN dates
WHERE NOT (dates.day = 7 AND r.github_repo_id % 13 = 0)
  AND NOT (r.github_repo_id % 20 = 0 AND dates.day > 0)`, nil},
		{`INSERT INTO topics (id, slug, name, created_at, updated_at)
VALUES (1,'coding-agents','Coding','2026-06-01T00:00:00Z','2026-06-01T00:00:00Z'),
       (2,'research-agents','Research','2026-06-01T00:00:00Z','2026-06-01T00:00:00Z'),
       (3,'agent-platforms','Platforms','2026-06-01T00:00:00Z','2026-06-01T00:00:00Z'),
       (4,'ai-infrastructure','Infrastructure','2026-06-01T00:00:00Z','2026-06-01T00:00:00Z')`, nil},
		{`INSERT INTO repository_topics (repository_id, topic_id, source, confidence, assigned_at, updated_at)
SELECT github_repo_id, github_repo_id % 4 + 1, 'auto', 0.9, '2026-06-01T00:00:00Z', '2026-06-01T00:00:00Z'
FROM repositories`, nil},
	}
	for _, statement := range statements {
		if _, err := transaction.ExecContext(ctx, statement.sql, statement.args...); err != nil {
			b.Fatal(err)
		}
	}
	if err := transaction.Commit(); err != nil {
		b.Fatal(err)
	}
}

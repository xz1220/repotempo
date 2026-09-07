package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

var _ corestore.RadarStore = (*Store)(nil)
var _ corestore.PreferredSnapshotDateStore = (*Store)(nil)
var _ corestore.LatestLibraryDateStore = (*Store)(nil)

func (store *Store) LatestLibraryDate(ctx context.Context) (domain.Date, error) {
	latest := domain.Date("")
	for _, readDate := range []func(context.Context) (domain.Date, error){store.LatestSnapshotDate, store.PreferredSnapshotDate} {
		candidate, err := readDate(ctx)
		if errors.Is(err, corestore.ErrNotFound) {
			continue
		}
		if err != nil {
			return "", err
		}
		if candidate > latest {
			latest = candidate
		}
	}
	var firstSeen sql.NullString
	if err := store.db.QueryRowContext(ctx, `SELECT MAX(date(first_seen_at, '+8 hours')) FROM repositories`).Scan(&firstSeen); err != nil {
		return "", fmt.Errorf("query latest registry date: %w", err)
	}
	if firstSeen.Valid {
		candidate, err := domain.ParseDate(firstSeen.String)
		if err != nil {
			return "", fmt.Errorf("parse latest registry date: %w", err)
		}
		if candidate > latest {
			latest = candidate
		}
	}
	if latest == "" {
		return "", corestore.ErrNotFound
	}
	return latest, nil
}

func (store *Store) PreferredSnapshotDate(ctx context.Context) (domain.Date, error) {
	// Failed and partial batches remain eligible: hiding a failed batch behind
	// an older successful date would conceal the collection problem. A batch
	// that failed before writing report.date still targeted its Shanghai start
	// date. Running batches and one-off watch operations are not defaults.
	var raw string
	err := store.db.QueryRowContext(ctx, `
WITH completed AS (
    SELECT started_at, finished_at,
           CASE WHEN job_type = 'run-daily' THEN json_extract(details_json, '$.snapshot.date')
                ELSE json_extract(details_json, '$.date') END AS reported_date
    FROM job_runs
    WHERE job_type IN ('snapshot', 'run-daily')
      AND status IN ('success', 'partial', 'failed') AND finished_at IS NOT NULL
), dated AS (
    SELECT COALESCE(CASE WHEN length(reported_date) = 10 AND date(reported_date) = reported_date
                        THEN reported_date END, date(started_at, '+8 hours')) AS target_date,
           finished_at
    FROM completed
)
SELECT target_date FROM dated WHERE target_date IS NOT NULL
ORDER BY target_date DESC, finished_at DESC LIMIT 1`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return store.LatestSnapshotDate(ctx)
	}
	if err != nil {
		return "", fmt.Errorf("query preferred snapshot date: %w", err)
	}
	value, err := domain.ParseDate(raw)
	if err != nil {
		return "", fmt.Errorf("parse preferred snapshot date: %w", err)
	}
	return value, nil
}

func (store *Store) RadarOverview(ctx context.Context, requested domain.RepositoryTrendQuery) (domain.RadarOverview, error) {
	query, err := normalizeTrendQuery(requested)
	if err != nil {
		return domain.RadarOverview{}, err
	}
	// The overview is scoped by topic, status, and focus. Search and discovery
	// filters belong to the repository list, not the population statistics.
	query.Search, query.OnlyNew, query.AfterID = "", false, nil
	query.Limit = 6
	baseline, _ := query.AsOf.AddDays(-query.WindowDays)
	previous, _ := baseline.AddDays(-query.WindowDays)
	result := domain.RadarOverview{
		AsOf: query.AsOf, BaselineDate: baseline, PreviousDate: previous, WindowDays: query.WindowDays,
		Fastest: []domain.RepositoryTrendMetric{}, Slowest: []domain.RepositoryTrendMetric{},
		FallingBehind: []domain.RepositoryTrendMetric{}, NewRepositories: []domain.RepositoryTrendMetric{},
		History: []domain.RadarHistoryPoint{},
	}
	scope, scopeArgs := trendScope(query)
	arguments := append([]any{}, scopeArgs...)
	arguments = append(arguments, query.AsOf, baseline, query.WindowDays, query.AsOf)
	coverage := domain.RadarCoverage{ComparisonCoverage: domain.ComparisonCoverage{BaselineDate: baseline, AsOfDate: query.AsOf}}
	statement := trendBaseCTE(scope) + `
SELECT COUNT(*), COUNT(current_stars), COUNT(star_delta), COALESCE(SUM(is_new), 0),
       COALESCE(SUM(CASE WHEN star_delta > 0 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN star_delta = 0 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN star_delta < 0 THEN 1 ELSE 0 END), 0),
       COUNT(momentum_change),
       COALESCE(SUM(CASE WHEN momentum_change < 0 THEN 1 ELSE 0 END), 0),
       COALESCE(SUM(CASE WHEN current_stars IS NULL AND last_observed_stars IS NOT NULL THEN 1 ELSE 0 END), 0),
       (SELECT COUNT(*) FROM daily_snapshots snapshot JOIN scope ON scope.github_repo_id = snapshot.repository_id
        WHERE snapshot.snapshot_date = (SELECT as_of_date FROM comparison_dates) AND snapshot.fetch_status = 'failed')
FROM observed`
	if err := store.db.QueryRowContext(ctx, statement, arguments...).Scan(
		&coverage.ScopeCount, &coverage.ObservedCount, &coverage.ComparableCount, &coverage.NewCount,
		&coverage.UpCount, &coverage.FlatCount, &coverage.DownCount, &coverage.MomentumComparableCount,
		&coverage.SlowingCount, &coverage.StaleCount, &coverage.FailedCount,
	); err != nil {
		return domain.RadarOverview{}, fmt.Errorf("query radar coverage: %w", err)
	}
	coverage.MissingCount = coverage.ScopeCount - coverage.ObservedCount - coverage.FailedCount
	result.Coverage = coverage
	for _, board := range []struct {
		sort        domain.RepositoryTrendSort
		newOnly     bool
		destination *[]domain.RepositoryTrendMetric
	}{
		{domain.RepositoryTrendSortDelta, false, &result.Fastest},
		{domain.RepositoryTrendSortLowGrowth, false, &result.Slowest},
		{domain.RepositoryTrendSortSlowdown, false, &result.FallingBehind},
		{domain.RepositoryTrendSortNewest, true, &result.NewRepositories},
	} {
		boardQuery := query
		boardQuery.Sort, boardQuery.OnlyNew = board.sort, board.newOnly
		page, err := store.ListRepositoryTrends(ctx, boardQuery)
		if err != nil {
			return domain.RadarOverview{}, fmt.Errorf("query radar %s board: %w", board.sort, err)
		}
		for _, repository := range page.Items {
			if !board.newOnly && repository.StarDelta == nil {
				continue
			}
			if board.sort == domain.RepositoryTrendSortDelta && *repository.StarDelta <= 0 {
				continue
			}
			if board.sort == domain.RepositoryTrendSortLowGrowth && *repository.StarDelta < 0 {
				continue
			}
			if board.sort == domain.RepositoryTrendSortSlowdown && (repository.MomentumChange == nil || *repository.MomentumChange >= 0) {
				continue
			}
			*board.destination = append(*board.destination, repository)
		}
	}
	result.History, err = store.radarHistory(ctx, query, baseline, scope, scopeArgs)
	if err != nil {
		return domain.RadarOverview{}, err
	}
	return result, nil
}

func (store *Store) radarHistory(ctx context.Context, query domain.RepositoryTrendQuery, baseline domain.Date, scope string, scopeArgs []any) ([]domain.RadarHistoryPoint, error) {
	statement := trendBaseCTE(scope) + `,
calendar(day) AS (
    SELECT baseline_date FROM comparison_dates
    UNION ALL
    SELECT date(day, '+1 day') FROM calendar WHERE day < (SELECT as_of_date FROM comparison_dates)
), cohort_summary AS (
    SELECT COUNT(*) AS cohort_count, SUM(baseline_stars) AS baseline_stars FROM common_cohort
)
SELECT calendar.day, summary.cohort_count, COUNT(snapshot.star_count),
       CASE WHEN summary.cohort_count > 0 AND COUNT(snapshot.star_count) = summary.cohort_count
            THEN SUM(snapshot.star_count) END AS stars,
       CASE WHEN summary.baseline_stars > 0 AND COUNT(snapshot.star_count) = summary.cohort_count
            THEN 100.0 * SUM(snapshot.star_count) / summary.baseline_stars END AS normalized_index
FROM calendar CROSS JOIN cohort_summary summary
LEFT JOIN daily_snapshots snapshot ON snapshot.snapshot_date = calendar.day
  AND snapshot.fetch_status = 'success'
  AND snapshot.repository_id IN (SELECT github_repo_id FROM common_cohort)
GROUP BY calendar.day, summary.cohort_count, summary.baseline_stars
ORDER BY calendar.day`
	arguments := append([]any{}, scopeArgs...)
	arguments = append(arguments, query.AsOf, baseline, query.WindowDays, query.AsOf)
	rows, err := store.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query fixed-cohort radar history: %w", err)
	}
	defer rows.Close()
	result := make([]domain.RadarHistoryPoint, 0, query.WindowDays+1)
	for rows.Next() {
		var point domain.RadarHistoryPoint
		var dateText string
		var stars sql.NullInt64
		var index sql.NullFloat64
		if err := rows.Scan(&dateText, &point.CohortCount, &point.ObservedCount, &stars, &index); err != nil {
			return nil, fmt.Errorf("scan radar history: %w", err)
		}
		point.Date, err = domain.ParseDate(dateText)
		if err != nil {
			return nil, err
		}
		point.Stars, point.Index = nullInt64Pointer(stars), nullFloat64Pointer(index)
		result = append(result, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate radar history: %w", err)
	}
	return result, nil
}

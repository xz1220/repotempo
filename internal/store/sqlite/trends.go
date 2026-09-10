package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const unclassifiedTopicFilter = "__unclassified"

type trendValues struct {
	CurrentStars      *int64
	BaselineStars     *int64
	CurrentRank       *int64
	BaselineRank      *int64
	RankChange        *int64
	StarDelta         *int64
	GrowthRate        *float64
	DailyVelocity     *float64
	IsNew             bool
	LastObservedDate  *domain.Date
	LastObservedStars *int64
	PreviousDelta     *int64
	MomentumChange    *int64
}

func (store *Store) LatestSnapshotDate(ctx context.Context) (domain.Date, error) {
	var raw sql.NullString
	if err := store.db.QueryRowContext(ctx, `
SELECT MAX(snapshot_date)
FROM daily_snapshots
WHERE fetch_status = 'success'`).Scan(&raw); err != nil {
		return "", fmt.Errorf("query latest snapshot date: %w", err)
	}
	if !raw.Valid || raw.String == "" {
		return "", corestore.ErrNotFound
	}
	value, err := domain.ParseDate(raw.String)
	if err != nil {
		return "", fmt.Errorf("parse latest snapshot date: %w", err)
	}
	return value, nil
}

func normalizeTrendQuery(query domain.RepositoryTrendQuery) (domain.RepositoryTrendQuery, error) {
	if err := query.AsOf.Validate(); err != nil {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	switch query.WindowDays {
	case 0:
		query.WindowDays = 7
	case 1, 7, 30:
	default:
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: comparison window must be 1, 7, or 30 days", corestore.ErrInvalid)
	}
	if query.DiscoverySource != "" && !query.DiscoverySource.Valid() {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: invalid discovery source %q", corestore.ErrInvalid, query.DiscoverySource)
	}
	if query.MonitoringStatus != "" && !query.MonitoringStatus.Valid() {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: invalid monitoring status %q", corestore.ErrInvalid, query.MonitoringStatus)
	}
	switch query.Sort {
	case "":
		query.Sort = domain.RepositoryTrendSortVelocity
	case domain.RepositoryTrendSortRankChange, domain.RepositoryTrendSortStars,
		domain.RepositoryTrendSortDelta, domain.RepositoryTrendSortGrowthRate,
		domain.RepositoryTrendSortVelocity, domain.RepositoryTrendSortLowGrowth,
		domain.RepositoryTrendSortSlowdown, domain.RepositoryTrendSortNewest,
		domain.RepositoryTrendSortName:
	default:
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: invalid trend sort %q", corestore.ErrInvalid, query.Sort)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if query.Offset < 0 {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: trend offset must not be negative", corestore.ErrInvalid)
	}
	if query.AfterID != nil && *query.AfterID <= 0 {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: cursor repository ID must be positive", corestore.ErrInvalid)
	}
	if query.AfterID != nil && query.Sort == domain.RepositoryTrendSortName {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: name sort does not support legacy cursors", corestore.ErrInvalid)
	}
	query.Search = strings.TrimSpace(query.Search)
	query.Tag = strings.ToLower(strings.TrimSpace(query.Tag))
	return query, nil
}

func trendScope(query domain.RepositoryTrendQuery) (string, []any) {
	statement := `
SELECT r.github_repo_id, r.full_name, r.description, r.first_seen_at
FROM repositories r
WHERE date(r.first_seen_at, '+8 hours') <= ?`
	arguments := []any{query.AsOf}
	if query.TopicSlug == unclassifiedTopicFilter {
		statement += ` AND NOT EXISTS (
            SELECT 1
            FROM repository_topics rt
            JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
            WHERE rt.repository_id = r.github_repo_id
              AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
        )`
	} else if query.TopicSlug != "" {
		statement += ` AND EXISTS (
            SELECT 1
            FROM repository_topics rt
            JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
            WHERE rt.repository_id = r.github_repo_id
              AND (t.slug = ? COLLATE NOCASE OR t.parent_id = (
                  SELECT id FROM topics WHERE slug = ? COLLATE NOCASE
              ))
              AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
        )`
		arguments = append(arguments, query.TopicSlug, query.TopicSlug)
	}
	if query.Tag != "" {
		statement += ` AND (
            EXISTS (SELECT 1 FROM json_each(COALESCE(r.github_topics_json, '[]')) label
                    WHERE label.type = 'text' AND label.value = ?)
            OR EXISTS (SELECT 1 FROM json_each(r.research_tags_json) label
                       WHERE label.type = 'text' AND label.value = ?)
            OR EXISTS (
                SELECT 1 FROM repository_topics rt
                JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
                WHERE rt.repository_id = r.github_repo_id
                  AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
                  AND (lower(trim(t.slug)) = ? OR lower(trim(t.name)) = ?)
            )
        )`
		arguments = append(arguments, query.Tag, query.Tag, query.Tag, query.Tag)
	}
	if query.DiscoverySource != "" {
		statement += " AND (r.first_seen_source = ? OR EXISTS (SELECT 1 FROM json_each(r.discovery_sources_json) channel WHERE channel.value = ?))"
		arguments = append(arguments, query.DiscoverySource, query.DiscoverySource)
	}
	if query.MonitoringStatus != "" {
		statement += " AND r.monitoring_status = ?"
		arguments = append(arguments, query.MonitoringStatus)
	}
	if query.OnlyFocus {
		statement += " AND r.is_focus = 1"
	}
	return statement, arguments
}

func trendFilteredWhere(query domain.RepositoryTrendQuery, alias string) (string, []any) {
	conditions := []string{"1 = 1"}
	arguments := []any{}
	if query.Search != "" {
		conditions = append(conditions, "(instr(lower("+alias+".full_name), lower(?)) > 0 OR instr(lower("+alias+".description), lower(?)) > 0)")
		arguments = append(arguments, query.Search, query.Search)
	}
	if query.OnlyNew {
		conditions = append(conditions, alias+".is_new = 1")
	}
	if query.Sort == domain.RepositoryTrendSortLowGrowth {
		conditions = append(conditions, alias+".star_delta >= 0")
	}
	if query.Sort == domain.RepositoryTrendSortSlowdown {
		conditions = append(conditions, alias+".momentum_change < 0")
	}
	return strings.Join(conditions, " AND "), arguments
}

// Rank within the comparable partition itself: joining a window-function CTE
// back by repository ID makes SQLite scan that CTE for every registry row.
// The separate cohort is used only by the fixed-population chart query.
func trendBaseCTE(scope string) string {
	return `
WITH scope AS (` + scope + `
), comparison_dates AS (
    SELECT ? AS as_of_date, ? AS baseline_date, ? AS window_days, ? AS new_date
), end_observation AS (
    SELECT repository_id, star_count
    FROM daily_snapshots
    WHERE snapshot_date = (SELECT as_of_date FROM comparison_dates) AND fetch_status = 'success'
), baseline_observation AS (
    SELECT repository_id, star_count
    FROM daily_snapshots
    WHERE snapshot_date = (SELECT baseline_date FROM comparison_dates) AND fetch_status = 'success'
), previous_observation AS (
    SELECT repository_id, star_count
    FROM daily_snapshots
    WHERE snapshot_date = (SELECT date(baseline_date, '-' || window_days || ' days') FROM comparison_dates)
      AND fetch_status = 'success'
), latest_observation AS (
    SELECT snapshot.repository_id, snapshot.star_count, snapshot.snapshot_date
    FROM scope
    CROSS JOIN daily_snapshots snapshot ON snapshot.repository_id = scope.github_repo_id
      AND snapshot.fetch_status = 'success'
      AND snapshot.snapshot_date = (
        SELECT MAX(history.snapshot_date) FROM daily_snapshots history
        WHERE history.repository_id = scope.github_repo_id AND history.fetch_status = 'success'
          AND history.snapshot_date <= (SELECT as_of_date FROM comparison_dates)
      )
), common_cohort AS (
    SELECT
        scope.github_repo_id,
        baseline.star_count AS baseline_stars,
        endpoint.star_count AS current_stars
    FROM scope
    JOIN end_observation endpoint ON endpoint.repository_id = scope.github_repo_id
    JOIN baseline_observation baseline ON baseline.repository_id = scope.github_repo_id
), observed AS (
    SELECT
        scope.github_repo_id,
        scope.full_name,
        scope.description,
        scope.first_seen_at,
        endpoint.star_count AS current_stars,
        latest.star_count AS last_observed_stars,
        latest.snapshot_date AS last_observed_date,
        baseline.star_count AS baseline_stars,
        CASE WHEN baseline.star_count IS NOT NULL
             THEN endpoint.star_count - baseline.star_count END AS star_delta,
        CASE WHEN baseline.star_count > 0
             THEN 1.0 * (endpoint.star_count - baseline.star_count) / baseline.star_count END AS growth_rate,
        CASE WHEN baseline.star_count IS NOT NULL
             THEN 1.0 * (endpoint.star_count - baseline.star_count) / (SELECT window_days FROM comparison_dates) END AS daily_velocity,
        CASE WHEN previous.star_count IS NOT NULL AND baseline.star_count IS NOT NULL
             THEN baseline.star_count - previous.star_count END AS previous_delta,
        CASE WHEN previous.star_count IS NOT NULL AND baseline.star_count IS NOT NULL
             THEN endpoint.star_count - 2 * baseline.star_count + previous.star_count END AS momentum_change,
        CASE WHEN date(scope.first_seen_at, '+8 hours') = (SELECT new_date FROM comparison_dates) THEN 1 ELSE 0 END AS is_new
    FROM scope
    LEFT JOIN end_observation endpoint ON endpoint.repository_id = scope.github_repo_id
    LEFT JOIN baseline_observation baseline ON baseline.repository_id = scope.github_repo_id
    LEFT JOIN previous_observation previous ON previous.repository_id = scope.github_repo_id
    LEFT JOIN latest_observation latest ON latest.repository_id = scope.github_repo_id
), scored AS (
    SELECT observed.*,
        CASE WHEN star_delta IS NOT NULL THEN DENSE_RANK() OVER current_window END AS current_rank,
        CASE WHEN star_delta IS NOT NULL THEN DENSE_RANK() OVER baseline_window END AS baseline_rank,
        CASE WHEN star_delta IS NOT NULL
             THEN DENSE_RANK() OVER baseline_window - DENSE_RANK() OVER current_window END AS rank_change
    FROM observed
    WINDOW current_window AS (PARTITION BY star_delta IS NULL ORDER BY current_stars DESC),
           baseline_window AS (PARTITION BY star_delta IS NULL ORDER BY baseline_stars DESC)
)`
}

func (store *Store) trendCoverage(
	ctx context.Context,
	query domain.RepositoryTrendQuery,
	baseline domain.Date,
	scope string,
	scopeArguments []any,
) (domain.ComparisonCoverage, trendPageCounts, error) {
	filteredWhere, filteredArguments := trendFilteredWhere(query, "scored")
	statement := trendBaseCTE(scope) + `
SELECT
    (SELECT COUNT(*) FROM scope),
    COUNT(current_stars),
    COUNT(star_delta),
    COALESCE(SUM(is_new), 0),
    COALESCE(SUM(CASE WHEN ` + filteredWhere + ` THEN 1 ELSE 0 END), 0),
    (SELECT COUNT(*) FROM repositories registry
     WHERE date(registry.first_seen_at, '+8 hours') <= (SELECT as_of_date FROM comparison_dates)),
    (SELECT COUNT(*) FROM repositories registry
     WHERE date(registry.first_seen_at, '+8 hours') <= (SELECT as_of_date FROM comparison_dates)
       AND registry.is_focus = 1)
FROM observed scored`
	arguments := append([]any{}, scopeArguments...)
	arguments = append(arguments, query.AsOf, baseline, query.WindowDays, query.AsOf)
	arguments = append(arguments, filteredArguments...)
	coverage := domain.ComparisonCoverage{BaselineDate: baseline, AsOfDate: query.AsOf}
	var counts trendPageCounts
	if err := store.db.QueryRowContext(ctx, statement, arguments...).Scan(
		&coverage.ScopeCount,
		&coverage.ObservedCount,
		&coverage.ComparableCount,
		&coverage.NewCount,
		&counts.Filtered,
		&counts.Registry,
		&counts.Focus,
	); err != nil {
		return domain.ComparisonCoverage{}, trendPageCounts{}, fmt.Errorf("query repository trend coverage: %w", err)
	}
	return coverage, counts, nil
}

type trendPageCounts struct {
	Filtered int
	Registry int
	Focus    int
}

func trendSortExpression(sortValue domain.RepositoryTrendSort, alias string) string {
	prefix := alias
	if prefix != "" {
		prefix += "."
	}
	switch sortValue {
	case domain.RepositoryTrendSortRankChange:
		return "ABS(" + prefix + "rank_change)"
	case domain.RepositoryTrendSortStars:
		return "COALESCE(" + prefix + "current_stars, " + prefix + "last_observed_stars)"
	case domain.RepositoryTrendSortDelta:
		return prefix + "star_delta"
	case domain.RepositoryTrendSortGrowthRate:
		return prefix + "growth_rate"
	case domain.RepositoryTrendSortLowGrowth:
		return "CASE WHEN " + prefix + "star_delta >= 0 THEN -" + prefix + "star_delta END"
	case domain.RepositoryTrendSortSlowdown:
		return "CASE WHEN " + prefix + "momentum_change < 0 THEN -" + prefix + "momentum_change END"
	case domain.RepositoryTrendSortNewest:
		return "julianday(" + prefix + "first_seen_at)"
	case domain.RepositoryTrendSortName:
		return "lower(" + prefix + "full_name)"
	default:
		return prefix + "daily_velocity"
	}
}

func trendSortDirection(sortValue domain.RepositoryTrendSort) string {
	if sortValue == domain.RepositoryTrendSortName {
		return "ASC"
	}
	return "DESC"
}

func (store *Store) ListRepositoryTrends(ctx context.Context, requested domain.RepositoryTrendQuery) (domain.RepositoryTrendPage, error) {
	query, err := normalizeTrendQuery(requested)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	baseline, err := query.AsOf.AddDays(-query.WindowDays)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	scope, scopeArguments := trendScope(query)
	coverage, counts, err := store.trendCoverage(ctx, query, baseline, scope, scopeArguments)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	page := domain.RepositoryTrendPage{
		Total:         counts.Filtered,
		RegistryTotal: counts.Registry,
		FocusTotal:    counts.Focus,
		Coverage:      coverage,
		Items:         []domain.RepositoryTrendMetric{},
	}
	if counts.Filtered == 0 {
		return page, nil
	}
	if query.AfterID == nil && query.Offset >= counts.Filtered {
		// The handler uses the real total to redirect an out-of-range page.
		// Avoid asking SQLite to walk an arbitrarily large hostile offset first.
		return page, nil
	}

	filteredWhere, filteredArguments := trendFilteredWhere(query, "scored")
	sortExpression := trendSortExpression(query.Sort, "filtered")
	statement := trendBaseCTE(scope) + `,
filtered AS (
    SELECT * FROM scored WHERE ` + filteredWhere + `
)`
	arguments := append([]any{}, scopeArguments...)
	arguments = append(arguments, query.AsOf, baseline, query.WindowDays, query.AsOf)
	arguments = append(arguments, filteredArguments...)
	if query.AfterID != nil {
		statement += `, cursor_values AS (
    SELECT ` + sortExpression + ` AS sort_value,
           (` + sortExpression + ` IS NULL) AS sort_is_null,
           COALESCE(filtered.current_stars, filtered.last_observed_stars, -1) AS sort_stars,
           filtered.github_repo_id
    FROM filtered
    WHERE filtered.github_repo_id = ?
)`
		arguments = append(arguments, *query.AfterID)
	}
	statement += `
SELECT
    filtered.github_repo_id,
    filtered.current_stars,
    filtered.baseline_stars,
    filtered.current_rank,
    filtered.baseline_rank,
    filtered.rank_change,
    filtered.star_delta,
    filtered.growth_rate,
    filtered.daily_velocity,
    filtered.is_new,
    filtered.last_observed_date,
    filtered.last_observed_stars,
    filtered.previous_delta,
    filtered.momentum_change
FROM filtered`
	if query.AfterID != nil {
		statement += ` CROSS JOIN cursor_values cursor
WHERE
    (` + sortExpression + ` IS NULL) > cursor.sort_is_null
    OR ((` + sortExpression + ` IS NULL) = cursor.sort_is_null AND (
        (cursor.sort_is_null = 0 AND ` + sortExpression + ` < cursor.sort_value)
        OR ((cursor.sort_is_null = 1 OR ` + sortExpression + ` = cursor.sort_value) AND (
            COALESCE(filtered.current_stars, filtered.last_observed_stars, -1) < cursor.sort_stars
            OR (COALESCE(filtered.current_stars, filtered.last_observed_stars, -1) = cursor.sort_stars AND filtered.github_repo_id > cursor.github_repo_id)
        ))
    ))`
	}
	statement += ` ORDER BY (` + sortExpression + ` IS NULL) ASC, ` + sortExpression + ` ` + trendSortDirection(query.Sort) + `,
    COALESCE(filtered.current_stars, filtered.last_observed_stars, -1) DESC, filtered.github_repo_id ASC
LIMIT ?`
	arguments = append(arguments, query.Limit+1)
	if query.AfterID == nil {
		statement += ` OFFSET ?`
		arguments = append(arguments, query.Offset)
	}

	rows, err := store.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return domain.RepositoryTrendPage{}, fmt.Errorf("query repository trends: %w", err)
	}
	defer rows.Close()
	repositoryIDs := make([]int64, 0, query.Limit+1)
	valuesByID := make(map[int64]trendValues, query.Limit+1)
	for rows.Next() {
		var repositoryID int64
		var current, baselineStars, currentRank, baselineRank, rankChange, delta sql.NullInt64
		var growthRate, velocity sql.NullFloat64
		var lastDate sql.NullString
		var lastStars, previousDelta, momentumChange sql.NullInt64
		var isNew int
		if err := rows.Scan(
			&repositoryID,
			&current,
			&baselineStars,
			&currentRank,
			&baselineRank,
			&rankChange,
			&delta,
			&growthRate,
			&velocity,
			&isNew,
			&lastDate,
			&lastStars,
			&previousDelta,
			&momentumChange,
		); err != nil {
			return domain.RepositoryTrendPage{}, fmt.Errorf("scan repository trend: %w", err)
		}
		var lastObservedDate *domain.Date
		if lastDate.Valid {
			parsed, err := domain.ParseDate(lastDate.String)
			if err != nil {
				return domain.RepositoryTrendPage{}, fmt.Errorf("parse last observed date: %w", err)
			}
			lastObservedDate = &parsed
		}
		repositoryIDs = append(repositoryIDs, repositoryID)
		valuesByID[repositoryID] = trendValues{
			CurrentStars:      nullInt64Pointer(current),
			BaselineStars:     nullInt64Pointer(baselineStars),
			CurrentRank:       nullInt64Pointer(currentRank),
			BaselineRank:      nullInt64Pointer(baselineRank),
			RankChange:        nullInt64Pointer(rankChange),
			StarDelta:         nullInt64Pointer(delta),
			GrowthRate:        nullFloat64Pointer(growthRate),
			DailyVelocity:     nullFloat64Pointer(velocity),
			IsNew:             isNew != 0,
			LastObservedDate:  lastObservedDate,
			LastObservedStars: nullInt64Pointer(lastStars),
			PreviousDelta:     nullInt64Pointer(previousDelta),
			MomentumChange:    nullInt64Pointer(momentumChange),
		}
	}
	if err := rows.Err(); err != nil {
		return domain.RepositoryTrendPage{}, fmt.Errorf("iterate repository trends: %w", err)
	}
	if len(repositoryIDs) > query.Limit {
		page.HasMore = true
		repositoryIDs = repositoryIDs[:query.Limit]
	}
	if len(repositoryIDs) == 0 {
		if query.AfterID != nil {
			return domain.RepositoryTrendPage{}, fmt.Errorf("%w: trend cursor is not part of the filtered result", corestore.ErrInvalid)
		}
		return page, nil
	}
	repositories, err := store.loadRepositoriesByIDs(ctx, repositoryIDs)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	topics, err := store.loadTopicsByRepositoryIDs(ctx, repositoryIDs)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	analyses, err := store.loadRepositoryAnalysesByIDs(ctx, repositoryIDs)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	for _, repositoryID := range repositoryIDs {
		repository, ok := repositories[repositoryID]
		if !ok {
			return domain.RepositoryTrendPage{}, fmt.Errorf("repository %d disappeared while loading trends", repositoryID)
		}
		values := valuesByID[repositoryID]
		page.Items = append(page.Items, domain.RepositoryTrendMetric{
			Repository:        repository,
			Topics:            topics[repositoryID],
			Analysis:          analyses[repositoryID],
			CurrentStars:      values.CurrentStars,
			BaselineStars:     values.BaselineStars,
			CurrentRank:       values.CurrentRank,
			BaselineRank:      values.BaselineRank,
			RankChange:        values.RankChange,
			StarDelta:         values.StarDelta,
			GrowthRate:        values.GrowthRate,
			DailyVelocity:     values.DailyVelocity,
			IsNew:             values.IsNew,
			LastObservedDate:  values.LastObservedDate,
			LastObservedStars: values.LastObservedStars,
			IsStale:           values.CurrentStars == nil,
			PreviousDelta:     values.PreviousDelta,
			MomentumChange:    values.MomentumChange,
		})
	}
	return page, nil
}

func nullFloat64Pointer(value sql.NullFloat64) *float64 {
	if !value.Valid {
		return nil
	}
	result := value.Float64
	return &result
}

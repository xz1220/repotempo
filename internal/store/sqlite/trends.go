package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
)

const unclassifiedTopicFilter = "__unclassified"

type trendValues struct {
	CurrentStars  *int64
	BaselineStars *int64
	CurrentRank   *int64
	BaselineRank  *int64
	RankChange    *int64
	StarDelta     *int64
	GrowthRate    *float64
	DailyVelocity *float64
	IsNew         bool
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
		domain.RepositoryTrendSortVelocity:
	default:
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: invalid trend sort %q", corestore.ErrInvalid, query.Sort)
	}
	if query.Limit <= 0 {
		query.Limit = 50
	}
	if query.Limit > 100 {
		query.Limit = 100
	}
	if query.AfterID != nil && *query.AfterID <= 0 {
		return domain.RepositoryTrendQuery{}, fmt.Errorf("%w: cursor repository ID must be positive", corestore.ErrInvalid)
	}
	query.Search = strings.TrimSpace(query.Search)
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
	if query.DiscoverySource != "" {
		statement += " AND r.first_seen_source = ?"
		arguments = append(arguments, query.DiscoverySource)
	}
	if query.MonitoringStatus != "" {
		statement += " AND r.monitoring_status = ?"
		arguments = append(arguments, query.MonitoringStatus)
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
	return strings.Join(conditions, " AND "), arguments
}

func trendBaseCTE(scope string) string {
	return `
WITH scope AS (` + scope + `
), end_observation AS (
    SELECT repository_id, star_count
    FROM daily_snapshots
    WHERE snapshot_date = ? AND fetch_status = 'success'
), baseline_observation AS (
    SELECT repository_id, star_count
    FROM daily_snapshots
    WHERE snapshot_date = ? AND fetch_status = 'success'
), common_ranked AS (
    SELECT
        scope.github_repo_id,
        baseline.star_count AS baseline_stars,
        endpoint.star_count AS current_stars,
        DENSE_RANK() OVER (ORDER BY baseline.star_count DESC) AS baseline_rank,
        DENSE_RANK() OVER (ORDER BY endpoint.star_count DESC) AS current_rank
    FROM scope
    JOIN end_observation endpoint ON endpoint.repository_id = scope.github_repo_id
    JOIN baseline_observation baseline ON baseline.repository_id = scope.github_repo_id
), scored AS (
    SELECT
        scope.github_repo_id,
        scope.full_name,
        scope.description,
        endpoint.star_count AS current_stars,
        baseline.star_count AS baseline_stars,
        ranked.current_rank,
        ranked.baseline_rank,
        ranked.baseline_rank - ranked.current_rank AS rank_change,
        CASE WHEN baseline.star_count IS NOT NULL
             THEN endpoint.star_count - baseline.star_count END AS star_delta,
        CASE WHEN baseline.star_count > 0
             THEN 1.0 * (endpoint.star_count - baseline.star_count) / baseline.star_count END AS growth_rate,
        CASE WHEN baseline.star_count IS NOT NULL
             THEN 1.0 * (endpoint.star_count - baseline.star_count) / ? END AS daily_velocity,
        CASE WHEN date(scope.first_seen_at, '+8 hours') = ? THEN 1 ELSE 0 END AS is_new
    FROM scope
    JOIN end_observation endpoint ON endpoint.repository_id = scope.github_repo_id
    LEFT JOIN baseline_observation baseline ON baseline.repository_id = scope.github_repo_id
    LEFT JOIN common_ranked ranked ON ranked.github_repo_id = scope.github_repo_id
)`
}

func (store *Store) trendCoverage(
	ctx context.Context,
	query domain.RepositoryTrendQuery,
	baseline domain.Date,
	scope string,
	scopeArguments []any,
) (domain.ComparisonCoverage, int, error) {
	filteredWhere, filteredArguments := trendFilteredWhere(query, "scored")
	statement := trendBaseCTE(scope) + `
SELECT
    (SELECT COUNT(*) FROM scope),
    COUNT(*),
    COALESCE(SUM(CASE WHEN baseline_stars IS NOT NULL THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(is_new), 0),
    COALESCE(SUM(CASE WHEN ` + filteredWhere + ` THEN 1 ELSE 0 END), 0)
FROM scored`
	arguments := append([]any{}, scopeArguments...)
	arguments = append(arguments, query.AsOf, baseline, query.WindowDays, query.AsOf)
	arguments = append(arguments, filteredArguments...)
	coverage := domain.ComparisonCoverage{BaselineDate: baseline, AsOfDate: query.AsOf}
	var total int
	if err := store.db.QueryRowContext(ctx, statement, arguments...).Scan(
		&coverage.ScopeCount,
		&coverage.ObservedCount,
		&coverage.ComparableCount,
		&coverage.NewCount,
		&total,
	); err != nil {
		return domain.ComparisonCoverage{}, 0, fmt.Errorf("query repository trend coverage: %w", err)
	}
	return coverage, total, nil
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
		return prefix + "current_stars"
	case domain.RepositoryTrendSortDelta:
		return prefix + "star_delta"
	case domain.RepositoryTrendSortGrowthRate:
		return prefix + "growth_rate"
	default:
		return prefix + "daily_velocity"
	}
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
	coverage, total, err := store.trendCoverage(ctx, query, baseline, scope, scopeArguments)
	if err != nil {
		return domain.RepositoryTrendPage{}, err
	}
	page := domain.RepositoryTrendPage{Total: total, Coverage: coverage, Items: []domain.RepositoryTrendMetric{}}
	if total == 0 {
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
           filtered.current_stars,
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
    filtered.is_new
FROM filtered`
	if query.AfterID != nil {
		statement += ` CROSS JOIN cursor_values cursor
WHERE
    (` + sortExpression + ` IS NULL) > cursor.sort_is_null
    OR ((` + sortExpression + ` IS NULL) = cursor.sort_is_null AND (
        (cursor.sort_is_null = 0 AND ` + sortExpression + ` < cursor.sort_value)
        OR ((cursor.sort_is_null = 1 OR ` + sortExpression + ` = cursor.sort_value) AND (
            filtered.current_stars < cursor.current_stars
            OR (filtered.current_stars = cursor.current_stars AND filtered.github_repo_id > cursor.github_repo_id)
        ))
    ))`
	}
	statement += ` ORDER BY (` + sortExpression + ` IS NULL) ASC, ` + sortExpression + ` DESC,
    filtered.current_stars DESC, filtered.github_repo_id ASC
LIMIT ?`
	arguments = append(arguments, query.Limit+1)

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
		); err != nil {
			return domain.RepositoryTrendPage{}, fmt.Errorf("scan repository trend: %w", err)
		}
		repositoryIDs = append(repositoryIDs, repositoryID)
		valuesByID[repositoryID] = trendValues{
			CurrentStars:  nullInt64Pointer(current),
			BaselineStars: nullInt64Pointer(baselineStars),
			CurrentRank:   nullInt64Pointer(currentRank),
			BaselineRank:  nullInt64Pointer(baselineRank),
			RankChange:    nullInt64Pointer(rankChange),
			StarDelta:     nullInt64Pointer(delta),
			GrowthRate:    nullFloat64Pointer(growthRate),
			DailyVelocity: nullFloat64Pointer(velocity),
			IsNew:         isNew != 0,
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
	for _, repositoryID := range repositoryIDs {
		repository, ok := repositories[repositoryID]
		if !ok {
			return domain.RepositoryTrendPage{}, fmt.Errorf("repository %d disappeared while loading trends", repositoryID)
		}
		values := valuesByID[repositoryID]
		page.Items = append(page.Items, domain.RepositoryTrendMetric{
			Repository:    repository,
			Topics:        topics[repositoryID],
			CurrentStars:  values.CurrentStars,
			BaselineStars: values.BaselineStars,
			CurrentRank:   values.CurrentRank,
			BaselineRank:  values.BaselineRank,
			RankChange:    values.RankChange,
			StarDelta:     values.StarDelta,
			GrowthRate:    values.GrowthRate,
			DailyVelocity: values.DailyVelocity,
			IsNew:         values.IsNew,
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

package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

type metricValues struct {
	Current   *int64
	Day       *int64
	SevenDay  *int64
	ThirtyDay *int64
}

func (values metricValues) domain() domain.GrowthMetric {
	return domain.GrowthMetric{
		Current:   values.Current,
		Day:       values.Day,
		SevenDay:  values.SevenDay,
		ThirtyDay: values.ThirtyDay,
	}
}

func normalizeMetricQuery(query domain.RepositoryMetricQuery, nowDate domain.Date) (domain.RepositoryMetricQuery, error) {
	if query.AsOf == "" {
		query.AsOf = nowDate
	}
	if err := query.AsOf.Validate(); err != nil {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	if query.RepositoryID != nil && *query.RepositoryID <= 0 {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: repository ID must be positive", corestore.ErrInvalid)
	}
	if query.DiscoverySource != "" && !query.DiscoverySource.Valid() {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: invalid discovery source %q", corestore.ErrInvalid, query.DiscoverySource)
	}
	if query.MonitoringStatus != "" && !query.MonitoringStatus.Valid() {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: invalid monitoring status %q", corestore.ErrInvalid, query.MonitoringStatus)
	}
	if query.GitHubStatus != "" && !query.GitHubStatus.Valid() {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: invalid GitHub status %q", corestore.ErrInvalid, query.GitHubStatus)
	}
	switch query.Sort {
	case "", domain.RepositorySortName, domain.RepositorySortStars, domain.RepositorySortDay,
		domain.RepositorySortSevenDay, domain.RepositorySortThirtyDay:
	default:
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: invalid repository sort %q", corestore.ErrInvalid, query.Sort)
	}
	if query.Sort == "" {
		query.Sort = domain.RepositorySortName
	}
	if query.Limit < 0 || query.Offset < 0 {
		return domain.RepositoryMetricQuery{}, fmt.Errorf("%w: pagination values cannot be negative", corestore.ErrInvalid)
	}
	return query, nil
}

func (store *Store) ListRepositoryMetrics(ctx context.Context, requested domain.RepositoryMetricQuery) ([]domain.RepositoryMetric, error) {
	nowDate := requested.AsOf
	if nowDate == "" {
		nowDate = domain.ShanghaiDate(store.nowUTC())
	}
	query, err := normalizeMetricQuery(requested, nowDate)
	if err != nil {
		return nil, err
	}
	dayDate, err := query.AsOf.AddDays(-1)
	if err != nil {
		return nil, err
	}
	sevenDayDate, err := query.AsOf.AddDays(-7)
	if err != nil {
		return nil, err
	}
	thirtyDayDate, err := query.AsOf.AddDays(-30)
	if err != nil {
		return nil, err
	}

	statement := `
WITH metrics AS (
    SELECT
        r.github_repo_id,
        r.full_name,
        (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) AS current_stars,
        (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) - (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) AS day_delta,
        (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) - (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) AS seven_day_delta,
        (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) - (
            SELECT s.star_count
            FROM daily_snapshots s
            WHERE s.repository_id = r.github_repo_id
              AND s.fetch_status = 'success'
              AND s.snapshot_date = ?
        ) AS thirty_day_delta
    FROM repositories r
    WHERE date(r.first_seen_at, '+8 hours') <= ?`
	arguments := []any{
		query.AsOf,
		query.AsOf, dayDate,
		query.AsOf, sevenDayDate,
		query.AsOf, thirtyDayDate,
		query.AsOf,
	}
	if query.RepositoryID != nil {
		statement += " AND r.github_repo_id = ?"
		arguments = append(arguments, *query.RepositoryID)
	}
	if strings.TrimSpace(query.Search) != "" {
		statement += " AND (instr(lower(r.full_name), lower(?)) > 0 OR instr(lower(r.description), lower(?)) > 0)"
		arguments = append(arguments, strings.TrimSpace(query.Search), strings.TrimSpace(query.Search))
	}
	if query.TopicSlug != "" {
		statement += ` AND EXISTS (
            SELECT 1 FROM repository_topics rt
            JOIN topics t ON t.id = rt.topic_id
            WHERE rt.repository_id = r.github_repo_id
              AND t.status = 'active'
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
	if query.GitHubStatus != "" {
		statement += " AND r.github_status = ?"
		arguments = append(arguments, query.GitHubStatus)
	}
	statement += ") SELECT github_repo_id, current_stars, day_delta, seven_day_delta, thirty_day_delta FROM metrics"

	sortColumn := map[domain.RepositorySort]string{
		domain.RepositorySortName:      "full_name COLLATE NOCASE",
		domain.RepositorySortStars:     "current_stars",
		domain.RepositorySortDay:       "day_delta",
		domain.RepositorySortSevenDay:  "seven_day_delta",
		domain.RepositorySortThirtyDay: "thirty_day_delta",
	}[query.Sort]
	direction := "ASC"
	if query.Descending {
		direction = "DESC"
	}
	statement += " ORDER BY " + sortColumn + " " + direction + ", full_name COLLATE NOCASE ASC"
	limit, offset, err := normalizePage(query.Limit, query.Offset)
	if err != nil {
		return nil, err
	}
	statement += " LIMIT ? OFFSET ?"
	arguments = append(arguments, limit, offset)

	rows, err := store.db.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query repository metrics: %w", err)
	}
	defer rows.Close()
	repositoryIDs := make([]int64, 0)
	valuesByID := make(map[int64]metricValues)
	for rows.Next() {
		var repositoryID int64
		var current sql.NullInt64
		var day sql.NullInt64
		var sevenDay sql.NullInt64
		var thirtyDay sql.NullInt64
		if err := rows.Scan(&repositoryID, &current, &day, &sevenDay, &thirtyDay); err != nil {
			return nil, fmt.Errorf("scan repository metrics: %w", err)
		}
		repositoryIDs = append(repositoryIDs, repositoryID)
		valuesByID[repositoryID] = metricValues{
			Current:   nullInt64Pointer(current),
			Day:       nullInt64Pointer(day),
			SevenDay:  nullInt64Pointer(sevenDay),
			ThirtyDay: nullInt64Pointer(thirtyDay),
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository metrics: %w", err)
	}
	if len(repositoryIDs) == 0 {
		return []domain.RepositoryMetric{}, nil
	}
	repositories, err := store.loadRepositoriesByIDs(ctx, repositoryIDs)
	if err != nil {
		return nil, err
	}
	topics, err := store.loadTopicsByRepositoryIDs(ctx, repositoryIDs)
	if err != nil {
		return nil, err
	}
	metrics := make([]domain.RepositoryMetric, 0, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		repository, ok := repositories[repositoryID]
		if !ok {
			return nil, fmt.Errorf("repository %d disappeared while loading metrics", repositoryID)
		}
		metrics = append(metrics, domain.RepositoryMetric{
			Repository: repository,
			Growth:     valuesByID[repositoryID].domain(),
			Topics:     topics[repositoryID],
		})
	}
	return metrics, nil
}

func nullInt64Pointer(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}

func chunks(values []int64, size int) [][]int64 {
	result := make([][]int64, 0, (len(values)+size-1)/size)
	for start := 0; start < len(values); start += size {
		end := start + size
		if end > len(values) {
			end = len(values)
		}
		result = append(result, values[start:end])
	}
	return result
}

func placeholders(count int) string {
	return strings.TrimSuffix(strings.Repeat("?,", count), ",")
}

func anyIDs(values []int64) []any {
	arguments := make([]any, len(values))
	for index, value := range values {
		arguments[index] = value
	}
	return arguments
}

func (store *Store) loadRepositoriesByIDs(ctx context.Context, repositoryIDs []int64) (map[int64]domain.Repository, error) {
	result := make(map[int64]domain.Repository, len(repositoryIDs))
	for _, chunk := range chunks(repositoryIDs, 500) {
		rows, err := store.db.QueryContext(ctx,
			"SELECT "+repositoryColumns+" FROM repositories WHERE github_repo_id IN ("+placeholders(len(chunk))+")",
			anyIDs(chunk)...,
		)
		if err != nil {
			return nil, fmt.Errorf("load metric repositories: %w", err)
		}
		for rows.Next() {
			repository, err := scanRepository(rows)
			if err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan metric repository: %w", err)
			}
			result[repository.GitHubRepoID] = repository
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate metric repositories: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close metric repository rows: %w", err)
		}
	}
	return result, nil
}

func (store *Store) loadTopicsByRepositoryIDs(ctx context.Context, repositoryIDs []int64) (map[int64][]domain.Topic, error) {
	result := make(map[int64][]domain.Topic, len(repositoryIDs))
	for _, repositoryID := range repositoryIDs {
		result[repositoryID] = []domain.Topic{}
	}
	for _, chunk := range chunks(repositoryIDs, 500) {
		rows, err := store.db.QueryContext(ctx, `
SELECT rt.repository_id, t.id, t.slug, t.name, t.parent_id, t.description, t.status, t.created_at, t.updated_at
FROM repository_topics rt
JOIN topics t ON t.id = rt.topic_id
WHERE rt.repository_id IN (`+placeholders(len(chunk))+`)
  AND t.status = 'active'
  AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
ORDER BY t.name COLLATE NOCASE`, anyIDs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("load metric topics: %w", err)
		}
		for rows.Next() {
			var repositoryID int64
			var topic domain.Topic
			var parentID sql.NullInt64
			var createdAt string
			var updatedAt string
			if err := rows.Scan(
				&repositoryID, &topic.ID, &topic.Slug, &topic.Name, &parentID,
				&topic.Description, &topic.Status, &createdAt, &updatedAt,
			); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan metric topic: %w", err)
			}
			if parentID.Valid {
				value := parentID.Int64
				topic.ParentID = &value
			}
			var err error
			topic.CreatedAt, err = parseStoredTime(createdAt)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			topic.UpdatedAt, err = parseStoredTime(updatedAt)
			if err != nil {
				_ = rows.Close()
				return nil, err
			}
			result[repositoryID] = append(result[repositoryID], topic)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("iterate metric topics: %w", err)
		}
		if err := rows.Close(); err != nil {
			return nil, fmt.Errorf("close metric topic rows: %w", err)
		}
	}
	return result, nil
}

func sumGrowth(metrics []domain.RepositoryMetric) domain.GrowthMetric {
	var result domain.GrowthMetric
	var current, day, sevenDay, thirtyDay int64
	var currentCount, dayCount, sevenDayCount, thirtyDayCount int
	for _, metric := range metrics {
		if metric.Growth.Current != nil {
			current += *metric.Growth.Current
			currentCount++
		}
		if metric.Growth.Day != nil {
			day += *metric.Growth.Day
			dayCount++
		}
		if metric.Growth.SevenDay != nil {
			sevenDay += *metric.Growth.SevenDay
			sevenDayCount++
		}
		if metric.Growth.ThirtyDay != nil {
			thirtyDay += *metric.Growth.ThirtyDay
			thirtyDayCount++
		}
	}
	if currentCount > 0 {
		result.Current = &current
	}
	if dayCount > 0 {
		result.Day = &day
	}
	if sevenDayCount > 0 {
		result.SevenDay = &sevenDay
	}
	if thirtyDayCount > 0 {
		result.ThirtyDay = &thirtyDay
	}
	return result
}

func topicMetric(topic domain.Topic, metrics []domain.RepositoryMetric) domain.TopicMetric {
	result := domain.TopicMetric{
		Topic:           topic,
		RepositoryCount: len(metrics),
		Growth:          sumGrowth(metrics),
	}
	for _, metric := range metrics {
		if metric.Growth.Day != nil {
			result.ComparableDay++
		}
		if metric.Growth.SevenDay != nil {
			result.ComparableSevenDay++
		}
		if metric.Growth.ThirtyDay != nil {
			result.ComparableThirtyDay++
		}
	}
	var leader *domain.RepositoryMetric
	for index := range metrics {
		metric := &metrics[index]
		if metric.Growth.Current == nil {
			continue
		}
		if leader == nil || *metric.Growth.Current > *leader.Growth.Current {
			leader = metric
		}
	}
	if leader != nil {
		leaderID := leader.Repository.GitHubRepoID
		result.LeaderRepositoryID = &leaderID
		result.LeaderFullName = leader.Repository.FullName
		if result.Growth.Current != nil && *result.Growth.Current > 0 {
			concentration := float64(*leader.Growth.Current) / float64(*result.Growth.Current)
			result.Concentration = &concentration
		}
	}
	return result
}

func (store *Store) growthHistory(ctx context.Context, asOf domain.Date) ([]domain.GrowthHistoryPoint, error) {
	from, err := asOf.AddDays(-29)
	if err != nil {
		return nil, err
	}
	rows, err := store.db.QueryContext(ctx, `
WITH RECURSIVE calendar(snapshot_date) AS (
    SELECT ?
    UNION ALL
    SELECT date(snapshot_date, '+1 day')
    FROM calendar
    WHERE snapshot_date < ?
), window_successes AS (
    SELECT
        s.repository_id,
        s.snapshot_date,
        s.star_count,
        previous.star_count AS previous_star_count,
        previous.snapshot_date AS previous_snapshot_date
    FROM daily_snapshots s
    JOIN daily_snapshots previous
      ON previous.repository_id = s.repository_id
     AND previous.fetch_status = 'success'
     AND previous.snapshot_date = (
        SELECT MAX(candidate.snapshot_date)
        FROM daily_snapshots candidate
        WHERE candidate.repository_id = s.repository_id
          AND candidate.fetch_status = 'success'
          AND candidate.snapshot_date < s.snapshot_date
     )
    WHERE s.fetch_status = 'success'
      AND s.snapshot_date >= ?
      AND s.snapshot_date <= ?
)
SELECT
    c.snapshot_date,
    CASE
        WHEN COUNT(w.repository_id) = 0 THEN NULL
        ELSE SUM(w.star_count - w.previous_star_count)
    END,
    COUNT(w.repository_id),
    COALESCE(SUM(CASE
        WHEN julianday(w.snapshot_date) - julianday(w.previous_snapshot_date) > 1 THEN 1
        ELSE 0
    END), 0)
FROM calendar c
LEFT JOIN window_successes w ON w.snapshot_date = c.snapshot_date
GROUP BY c.snapshot_date
ORDER BY c.snapshot_date`, from, asOf, from, asOf)
	if err != nil {
		return nil, fmt.Errorf("query growth history: %w", err)
	}
	defer rows.Close()

	points := make([]domain.GrowthHistoryPoint, 0, 30)
	for rows.Next() {
		var dateText string
		var delta sql.NullInt64
		var comparableCount int
		var gapSpanningCount int
		if err := rows.Scan(&dateText, &delta, &comparableCount, &gapSpanningCount); err != nil {
			return nil, fmt.Errorf("scan growth history: %w", err)
		}
		date, err := domain.ParseDate(dateText)
		if err != nil {
			return nil, fmt.Errorf("parse growth history date: %w", err)
		}
		points = append(points, domain.GrowthHistoryPoint{
			Date:                       date,
			Delta:                      nullInt64Pointer(delta),
			ComparableRepositoryCount:  comparableCount,
			GapSpanningRepositoryCount: gapSpanningCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate growth history: %w", err)
	}
	return points, nil
}

func (store *Store) DashboardSummary(ctx context.Context, asOf domain.Date) (domain.DashboardSummary, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	if err := asOf.Validate(); err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	summary := domain.DashboardSummary{}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repositories").Scan(&summary.RepositoryCount); err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("count repositories: %w", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM repositories WHERE monitoring_status = 'active'").Scan(&summary.ActiveCount); err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("count active repositories: %w", err)
	}
	if err := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM topics WHERE status = 'active'").Scan(&summary.TopicCount); err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("count active topics: %w", err)
	}
	summary.Coverage.Date = asOf
	if err := store.db.QueryRowContext(ctx, `
SELECT
    COUNT(r.github_repo_id),
    COALESCE(SUM(CASE WHEN s.fetch_status = 'success' THEN 1 ELSE 0 END), 0),
    COALESCE(SUM(CASE WHEN s.fetch_status = 'failed' THEN 1 ELSE 0 END), 0)
FROM repositories r
LEFT JOIN daily_snapshots s
  ON s.repository_id = r.github_repo_id AND s.snapshot_date = ?
WHERE r.monitoring_status = 'active'`, asOf).Scan(
		&summary.Coverage.TargetCount,
		&summary.Coverage.SuccessCount,
		&summary.Coverage.FailureCount,
	); err != nil {
		return domain.DashboardSummary{}, fmt.Errorf("query daily coverage: %w", err)
	}
	summary.Coverage.MissingCount = summary.Coverage.TargetCount - summary.Coverage.SuccessCount - summary.Coverage.FailureCount
	if summary.Coverage.TargetCount > 0 {
		summary.Coverage.Percent = float64(summary.Coverage.SuccessCount) * 100 / float64(summary.Coverage.TargetCount)
	}
	allMetrics, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:             asOf,
		MonitoringStatus: domain.MonitoringActive,
	})
	if err != nil {
		return domain.DashboardSummary{}, err
	}
	summary.Growth = sumGrowth(allMetrics)
	summary.GrowthHistory, err = store.growthHistory(ctx, asOf)
	if err != nil {
		return domain.DashboardSummary{}, err
	}
	summary.Fastest, err = store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:             asOf,
		MonitoringStatus: domain.MonitoringActive,
		Sort:             domain.RepositorySortDay,
		Descending:       true,
		Limit:            10,
	})
	if err != nil {
		return domain.DashboardSummary{}, err
	}
	summary.RecentRuns, err = store.ListJobRuns(ctx, 5, 0)
	if err != nil {
		return domain.DashboardSummary{}, err
	}
	return summary, nil
}

func (store *Store) GetRepositoryDetail(ctx context.Context, repositoryID int64, asOf domain.Date) (domain.RepositoryDetail, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	metrics, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:         asOf,
		RepositoryID: &repositoryID,
		Limit:        1,
	})
	if err != nil {
		return domain.RepositoryDetail{}, err
	}
	if len(metrics) == 0 {
		return domain.RepositoryDetail{}, corestore.ErrNotFound
	}
	history, err := store.ListDailySnapshots(ctx, repositoryID, domain.SnapshotFilter{Through: asOf})
	if err != nil {
		return domain.RepositoryDetail{}, err
	}
	analysis, err := store.getRepositoryAnalysis(ctx, repositoryID)
	if err != nil {
		return domain.RepositoryDetail{}, err
	}
	detail := domain.RepositoryDetail{
		Metric:      metrics[0],
		Analysis:    analysis,
		History:     history,
		FailedDates: []domain.Date{},
	}
	for _, snapshot := range history {
		if snapshot.FetchStatus == domain.FetchFailed {
			detail.FailedDates = append(detail.FailedDates, snapshot.SnapshotDate)
		}
		if detail.ValidFrom == nil && snapshot.FetchStatus == domain.FetchSuccess {
			date := snapshot.SnapshotDate
			detail.ValidFrom = &date
		}
	}
	return detail, nil
}

func (store *Store) ListTopicMetrics(ctx context.Context, asOf domain.Date) ([]domain.TopicMetric, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	topics, err := store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return nil, err
	}
	results := make([]domain.TopicMetric, 0, len(topics))
	for _, topic := range topics {
		metrics, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{AsOf: asOf, TopicSlug: topic.Slug})
		if err != nil {
			return nil, err
		}
		results = append(results, topicMetric(topic, metrics))
	}
	return results, nil
}

func (store *Store) TopicClassificationCoverage(ctx context.Context, asOf domain.Date) (domain.TopicClassificationCoverage, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	if err := asOf.Validate(); err != nil {
		return domain.TopicClassificationCoverage{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	var coverage domain.TopicClassificationCoverage
	if err := store.db.QueryRowContext(ctx, `
SELECT
    COUNT(*),
    COALESCE(SUM(CASE WHEN EXISTS (
        SELECT 1
        FROM repository_topics rt
        JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
        WHERE rt.repository_id = r.github_repo_id
          AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
    ) THEN 1 ELSE 0 END), 0)
FROM repositories r
WHERE date(r.first_seen_at, '+8 hours') <= ?`, asOf).Scan(
		&coverage.RepositoryCount,
		&coverage.ClassifiedCount,
	); err != nil {
		return domain.TopicClassificationCoverage{}, fmt.Errorf("query topic classification coverage: %w", err)
	}
	coverage.UnclassifiedCount = coverage.RepositoryCount - coverage.ClassifiedCount
	return coverage, nil
}

func (store *Store) GetTopicDetail(ctx context.Context, slug string, asOf domain.Date, excludeLeader bool) (domain.TopicDetail, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	if err := asOf.Validate(); err != nil {
		return domain.TopicDetail{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	topic, err := store.GetTopicBySlug(ctx, slug)
	if err != nil {
		return domain.TopicDetail{}, err
	}
	metrics, err := store.ListRepositoryMetrics(ctx, domain.RepositoryMetricQuery{
		AsOf:       asOf,
		TopicSlug:  slug,
		Sort:       domain.RepositorySortDay,
		Descending: true,
	})
	if err != nil {
		return domain.TopicDetail{}, err
	}
	originalMetric := topicMetric(topic, metrics)
	detail := domain.TopicDetail{Metric: originalMetric, Repositories: metrics, ExcludedLeader: excludeLeader}
	var excludedID *int64
	if excludeLeader && originalMetric.LeaderRepositoryID != nil {
		value := *originalMetric.LeaderRepositoryID
		excludedID = &value
		detail.ExcludedRepositoryID = &value
		detail.ExcludedRepositoryName = originalMetric.LeaderFullName
		filtered := make([]domain.RepositoryMetric, 0, len(metrics)-1)
		for _, metric := range metrics {
			if metric.Repository.GitHubRepoID != value {
				filtered = append(filtered, metric)
			}
		}
		detail.Repositories = filtered
		detail.Metric = topicMetric(topic, filtered)
	}
	detail.History, err = store.topicHistory(ctx, topic.ID, asOf, excludedID)
	if err != nil {
		return domain.TopicDetail{}, err
	}
	detail.Fastest = detail.Repositories
	if len(detail.Fastest) > 10 {
		detail.Fastest = detail.Fastest[:10]
	}
	return detail, nil
}

func (store *Store) topicHistory(ctx context.Context, topicID int64, asOf domain.Date, excludedID *int64) ([]domain.TopicHistoryPoint, error) {
	from, err := asOf.AddDays(-29)
	if err != nil {
		return nil, err
	}
	scope := `
    SELECT DISTINCT rt.repository_id
    FROM repository_topics rt
    JOIN topics assigned ON assigned.id = rt.topic_id
    WHERE (rt.topic_id = ? OR assigned.parent_id = ?)
      AND assigned.status = 'active'
      AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)`
	scopeArguments := []any{topicID, topicID}
	if excludedID != nil {
		scope += " AND rt.repository_id <> ?"
		scopeArguments = append(scopeArguments, *excludedID)
	}
	var startText sql.NullString
	startArguments := []any{asOf}
	startArguments = append(startArguments, scopeArguments...)
	startArguments = append(startArguments, from, asOf)
	if err := store.db.QueryRowContext(ctx, `
SELECT MIN(s.snapshot_date)
FROM daily_snapshots s
JOIN daily_snapshots endpoint
  ON endpoint.repository_id = s.repository_id
 AND endpoint.snapshot_date = ?
 AND endpoint.fetch_status = 'success'
WHERE s.repository_id IN (`+scope+`)
  AND s.fetch_status = 'success'
  AND s.snapshot_date >= ?
  AND s.snapshot_date <= ?`, startArguments...).Scan(&startText); err != nil {
		return nil, fmt.Errorf("query topic history start: %w", err)
	}
	if !startText.Valid || startText.String == "" {
		return []domain.TopicHistoryPoint{}, nil
	}
	start, err := domain.ParseDate(startText.String)
	if err != nil {
		return nil, fmt.Errorf("parse topic history start: %w", err)
	}
	query := `
WITH RECURSIVE calendar(snapshot_date) AS (
    SELECT ?
    UNION ALL
    SELECT date(snapshot_date, '+1 day')
    FROM calendar
    WHERE snapshot_date < ?
), scoped_repositories AS (` + scope + `
), cohort AS (
    SELECT scoped.repository_id
    FROM scoped_repositories scoped
    JOIN daily_snapshots baseline
      ON baseline.repository_id = scoped.repository_id
     AND baseline.snapshot_date = ?
     AND baseline.fetch_status = 'success'
    JOIN daily_snapshots endpoint
      ON endpoint.repository_id = scoped.repository_id
     AND endpoint.snapshot_date = ?
     AND endpoint.fetch_status = 'success'
)
SELECT
    calendar.snapshot_date,
    SUM(CASE WHEN observation.fetch_status = 'success' THEN observation.star_count END),
    COALESCE(SUM(CASE WHEN observation.fetch_status = 'success' THEN 1 ELSE 0 END), 0),
    (SELECT COUNT(*) FROM cohort)
FROM calendar
LEFT JOIN cohort ON 1 = 1
LEFT JOIN daily_snapshots observation
  ON observation.repository_id = cohort.repository_id
 AND observation.snapshot_date = calendar.snapshot_date
GROUP BY calendar.snapshot_date
ORDER BY calendar.snapshot_date`
	arguments := []any{start, asOf}
	arguments = append(arguments, scopeArguments...)
	arguments = append(arguments, start, asOf)
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query topic history: %w", err)
	}
	defer rows.Close()
	points := make([]domain.TopicHistoryPoint, 0)
	for rows.Next() {
		var dateText string
		var stars sql.NullInt64
		var observed int
		var targetCount int
		if err := rows.Scan(&dateText, &stars, &observed, &targetCount); err != nil {
			return nil, fmt.Errorf("scan topic history: %w", err)
		}
		date, err := domain.ParseDate(dateText)
		if err != nil {
			return nil, err
		}
		points = append(points, domain.TopicHistoryPoint{
			Date:          date,
			StarCount:     nullInt64Pointer(stars),
			ObservedCount: observed,
			TargetCount:   targetCount,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topic history: %w", err)
	}
	return points, nil
}

func (store *Store) DiscoverySummary(ctx context.Context) (domain.DiscoverySummary, error) {
	summary := domain.DiscoverySummary{
		Sources:          make([]domain.DiscoverySourceCount, 0, 4),
		FirstSeenSources: make([]domain.DiscoverySourceCount, 0, 4),
		Profiles:         []domain.DiscoveryProfileCount{},
		Runs:             []domain.JobRun{},
	}
	for _, source := range []domain.DiscoverySource{
		domain.DiscoverySourceOSSInsight,
		domain.DiscoverySourceGitHubSearch,
		domain.DiscoverySourceLegacy,
		domain.DiscoverySourceManual,
	} {
		var count int
		if err := store.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM repositories r
WHERE EXISTS (SELECT 1 FROM json_each(r.discovery_sources_json) WHERE value = ?)`, source).Scan(&count); err != nil {
			return domain.DiscoverySummary{}, fmt.Errorf("count %s discoveries: %w", source, err)
		}
		summary.Sources = append(summary.Sources, domain.DiscoverySourceCount{Source: source, RepositoryCount: count})
		if err := store.db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM repositories WHERE first_seen_source = ?", source,
		).Scan(&count); err != nil {
			return domain.DiscoverySummary{}, fmt.Errorf("count %s first-seen discoveries: %w", source, err)
		}
		summary.FirstSeenSources = append(summary.FirstSeenSources, domain.DiscoverySourceCount{
			Source:          source,
			RepositoryCount: count,
		})
	}
	rows, err := store.db.QueryContext(ctx, `
SELECT first_seen_profile, COUNT(*)
FROM repositories
WHERE first_seen_profile <> ''
GROUP BY first_seen_profile
ORDER BY COUNT(*) DESC, first_seen_profile`)
	if err != nil {
		return domain.DiscoverySummary{}, fmt.Errorf("query discovery profiles: %w", err)
	}
	for rows.Next() {
		var profile domain.DiscoveryProfileCount
		if err := rows.Scan(&profile.Profile, &profile.RepositoryCount); err != nil {
			_ = rows.Close()
			return domain.DiscoverySummary{}, fmt.Errorf("scan discovery profile: %w", err)
		}
		summary.Profiles = append(summary.Profiles, profile)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domain.DiscoverySummary{}, fmt.Errorf("iterate discovery profiles: %w", err)
	}
	if err := rows.Close(); err != nil {
		return domain.DiscoverySummary{}, fmt.Errorf("close discovery profile rows: %w", err)
	}
	runRows, err := store.db.QueryContext(ctx, `
SELECT `+jobRunColumns+`
FROM job_runs
WHERE job_type LIKE 'discover%'
ORDER BY started_at DESC
LIMIT 50`)
	if err != nil {
		return domain.DiscoverySummary{}, fmt.Errorf("query discovery runs: %w", err)
	}
	defer runRows.Close()
	for runRows.Next() {
		run, err := scanJobRun(runRows)
		if err != nil {
			return domain.DiscoverySummary{}, fmt.Errorf("scan discovery run: %w", err)
		}
		summary.Runs = append(summary.Runs, run)
	}
	if err := runRows.Err(); err != nil {
		return domain.DiscoverySummary{}, fmt.Errorf("iterate discovery runs: %w", err)
	}
	return summary, nil
}

package legacydb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/source"
)

type Options struct {
	Location *time.Location
}

type Stats struct {
	RepositoryRows         int
	CatalogRows            int
	ObservationRows        int
	ImportedObservations   int
	IgnoredTrendSnapshots  int
	IgnoredDaySnapshots    int
	IgnoredRepositoryStars int
}

type Result struct {
	Candidates   []source.Candidate
	Observations []source.Observation
	Warnings     []source.Warning
	Stats        Stats
}

type catalogEntry struct {
	AddedOn string
	AddedAt string
	Source  string
}

// Import reads the known ossinsight-feishu-digest SQLite schema. All rows in
// repos are candidates; catalog_projects only adds focus/provenance metadata.
// Only non-NULL daily_observations.github_stars values become observations.
// Rolling trend snapshots and undated repository totals are intentionally not
// converted into absolute-star history.
func Import(ctx context.Context, database *sql.DB, options Options) (Result, error) {
	if database == nil {
		return Result{}, errors.New("legacy database is nil")
	}
	if options.Location == nil {
		options.Location = time.UTC
	}
	result := Result{}
	catalog, warnings, err := readCatalog(ctx, database)
	if err != nil {
		return result, err
	}
	result.Warnings = append(result.Warnings, warnings...)
	result.Stats.CatalogRows = len(catalog)

	candidates, repoWarnings, err := readRepositories(ctx, database, catalog, options.Location)
	if err != nil {
		return result, err
	}
	result.Candidates = candidates
	result.Warnings = append(result.Warnings, repoWarnings...)
	result.Stats.RepositoryRows = countRows(ctx, database, "repos")
	result.Stats.IgnoredRepositoryStars = len(candidates)

	observations, observationWarnings, rowsRead, err := readObservations(ctx, database, options.Location)
	if err != nil {
		return result, err
	}
	result.Observations = observations
	result.Warnings = append(result.Warnings, observationWarnings...)
	result.Stats.ObservationRows = rowsRead
	result.Stats.ImportedObservations = len(observations)
	result.Stats.IgnoredTrendSnapshots = countRows(ctx, database, "trend_snapshots")
	result.Stats.IgnoredDaySnapshots = countRows(ctx, database, "day_snapshots")
	return result, nil
}

func readCatalog(ctx context.Context, database *sql.DB) (map[int64]catalogEntry, []source.Warning, error) {
	result := make(map[int64]catalogEntry)
	if !tableExists(ctx, database, "catalog_projects") {
		return result, []source.Warning{{Code: "missing_catalog_projects", Message: "catalog_projects table is absent; no focus markers imported"}}, nil
	}
	rows, err := database.QueryContext(ctx, `SELECT repo_id, COALESCE(added_on, ''), COALESCE(added_at, ''), COALESCE(source, '') FROM catalog_projects`)
	if err != nil {
		return nil, nil, fmt.Errorf("query legacy catalog_projects: %w", err)
	}
	defer rows.Close()
	warnings := make([]source.Warning, 0)
	rowNumber := 0
	for rows.Next() {
		rowNumber++
		var repoID int64
		var entry catalogEntry
		if err := rows.Scan(&repoID, &entry.AddedOn, &entry.AddedAt, &entry.Source); err != nil {
			return nil, warnings, fmt.Errorf("scan legacy catalog_projects row %d: %w", rowNumber, err)
		}
		if repoID <= 0 {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_catalog_repo_id", Message: "catalog repository ID must be positive"})
			continue
		}
		result[repoID] = entry
	}
	if err := rows.Err(); err != nil {
		return nil, warnings, fmt.Errorf("iterate legacy catalog_projects: %w", err)
	}
	return result, warnings, nil
}

func readRepositories(ctx context.Context, database *sql.DB, catalog map[int64]catalogEntry, location *time.Location) ([]source.Candidate, []source.Warning, error) {
	if !tableExists(ctx, database, "repos") {
		return nil, nil, errors.New("legacy database has no repos table")
	}
	rows, err := database.QueryContext(ctx, `
		SELECT repo_id,
		       COALESCE(repo_name, ''),
		       COALESCE(primary_language, ''),
		       COALESCE(description, ''),
		       COALESCE(html_url, ''),
		       COALESCE(github_topics, '[]'),
		       COALESCE(github_status, ''),
		       COALESCE(archived, 0),
		       COALESCE(metadata_etag, ''),
		       COALESCE(first_seen_at, ''),
		       COALESCE(last_seen_at, ''),
		       COALESCE(is_active, 1),
		       COALESCE(manual_favorite, 0)
		FROM repos`)
	if err != nil {
		return nil, nil, fmt.Errorf("query legacy repos: %w", err)
	}
	defer rows.Close()
	candidates := make([]source.Candidate, 0)
	warnings := make([]source.Warning, 0)
	seen := make(map[int64]struct{})
	rowNumber := 0
	for rows.Next() {
		rowNumber++
		var (
			repoID, archived, active, favorite       int64
			fullName, language, description, htmlURL string
			topicsJSON, githubStatus, etag           string
			firstSeenRaw, lastSeenRaw                string
		)
		if err := rows.Scan(&repoID, &fullName, &language, &description, &htmlURL, &topicsJSON, &githubStatus, &archived, &etag, &firstSeenRaw, &lastSeenRaw, &active, &favorite); err != nil {
			return candidates, warnings, fmt.Errorf("scan legacy repos row %d: %w", rowNumber, err)
		}
		if repoID <= 0 || !validFullName(fullName) {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_repository", Message: fmt.Sprintf("invalid repo_id/full_name: %d %q", repoID, fullName)})
			continue
		}
		if _, duplicate := seen[repoID]; duplicate {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "duplicate_repository_id", Message: strconv.FormatInt(repoID, 10)})
			continue
		}
		seen[repoID] = struct{}{}
		firstSeen, firstErr := parseLegacyTime(firstSeenRaw, location)
		if firstErr != nil {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_first_seen_at", Message: firstErr.Error()})
		}
		lastSeen, lastErr := parseLegacyTime(lastSeenRaw, location)
		if lastErr != nil {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_last_seen_at", Message: lastErr.Error()})
		}
		topics := parseStringList(topicsJSON)
		metadata := map[string]string{
			"legacy_github_status": githubStatus,
			"legacy_etag":          etag,
			"etag":                 etag,
			"legacy_is_active":     strconv.FormatBool(active != 0),
		}
		if lastSeen != nil {
			metadata["legacy_last_seen_at"] = lastSeen.UTC().Format(time.RFC3339)
		}
		catalogInfo, isCatalog := catalog[repoID]
		if isCatalog {
			metadata["legacy_catalog_source"] = catalogInfo.Source
			metadata["legacy_catalog_added_on"] = catalogInfo.AddedOn
			metadata["legacy_catalog_added_at"] = catalogInfo.AddedAt
		}
		discoveredAt := time.Time{}
		if firstSeen != nil {
			discoveredAt = firstSeen.UTC()
		}
		candidate := source.Candidate{
			Repository: source.Repository{
				ID:           repoID,
				FullName:     fullName,
				HTMLURL:      htmlURL,
				Description:  description,
				Language:     language,
				Archived:     archived != 0,
				Private:      strings.EqualFold(githubStatus, "private"),
				GitHubStatus: normalizeGitHubStatus(githubStatus, archived != 0),
				Topics:       topics,
				// repos.github_stars has no trustworthy observation timestamp.
				AbsoluteStars: nil,
			},
			Source:       "legacy",
			DiscoveredAt: discoveredAt,
			IsFocus:      isCatalog || favorite != 0,
			// The legacy is_active flag meant "currently present in a trend
			// window or manually favored", not "pause future monitoring".
			// Import every reachable repository into the fixed panel.
			MonitorStatus: "",
			Metadata:      metadata,
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return candidates, warnings, fmt.Errorf("iterate legacy repos: %w", err)
	}
	for repoID := range catalog {
		if _, ok := seen[repoID]; !ok {
			warnings = append(warnings, source.Warning{Code: "catalog_repository_missing", Message: fmt.Sprintf("catalog repository ID %d is absent from repos and was not synthesized", repoID)})
		}
	}
	return candidates, warnings, nil
}

func readObservations(ctx context.Context, database *sql.DB, location *time.Location) ([]source.Observation, []source.Warning, int, error) {
	if !tableExists(ctx, database, "daily_observations") {
		return nil, []source.Warning{{Code: "missing_daily_observations", Message: "daily_observations table is absent"}}, 0, nil
	}
	rows, err := database.QueryContext(ctx, `
		SELECT repo_id,
		       COALESCE(observation_date, ''),
		       COALESCE(observed_at, ''),
		       github_stars,
		       rank,
		       COALESCE(status, ''),
		       COALESCE(in_trending, 0),
		       window_stars
		FROM daily_observations
		WHERE github_stars IS NOT NULL`)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("query legacy daily_observations: %w", err)
	}
	defer rows.Close()
	observations := make([]source.Observation, 0)
	warnings := make([]source.Warning, 0)
	rowNumber := 0
	seen := make(map[string]struct{})
	for rows.Next() {
		rowNumber++
		var (
			repoID, githubStars, inTrending        int64
			observationDate, observedAtRaw, status string
			rank, windowStars                      sql.NullInt64
		)
		if err := rows.Scan(&repoID, &observationDate, &observedAtRaw, &githubStars, &rank, &status, &inTrending, &windowStars); err != nil {
			return observations, warnings, rowNumber, fmt.Errorf("scan legacy daily_observations row %d: %w", rowNumber, err)
		}
		if repoID <= 0 || githubStars < 0 {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_observation", Message: "repository ID must be positive and stars non-negative"})
			continue
		}
		date, err := time.ParseInLocation("2006-01-02", observationDate, location)
		if err != nil {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "invalid_observation_date", Message: err.Error()})
			continue
		}
		observedAt, err := parseLegacyTime(observedAtRaw, location)
		if err != nil || observedAt == nil {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "missing_observed_at", Message: "absolute star value omitted because its actual observation time is unavailable"})
			continue
		}
		key := fmt.Sprintf("%d/%s", repoID, observationDate)
		if _, duplicate := seen[key]; duplicate {
			warnings = append(warnings, source.Warning{Row: rowNumber, Code: "duplicate_observation", Message: key})
			continue
		}
		seen[key] = struct{}{}
		metadata := map[string]string{
			"legacy_status":      status,
			"legacy_in_trending": strconv.FormatBool(inTrending != 0),
		}
		if rank.Valid {
			metadata["legacy_rank"] = strconv.FormatInt(rank.Int64, 10)
		}
		if windowStars.Valid {
			metadata["legacy_window_stars"] = strconv.FormatInt(windowStars.Int64, 10)
		}
		observations = append(observations, source.Observation{
			RepositoryID: repoID,
			SnapshotDate: date,
			ObservedAt:   observedAt.UTC(),
			StarCount:    githubStars,
			Source:       "legacy_daily_observations",
			FixedPanel:   false,
			Metadata:     metadata,
		})
	}
	if err := rows.Err(); err != nil {
		return observations, warnings, rowNumber, fmt.Errorf("iterate legacy daily_observations: %w", err)
	}
	return observations, warnings, rowNumber, nil
}

func tableExists(ctx context.Context, database *sql.DB, table string) bool {
	var count int
	err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&count)
	return err == nil && count > 0
}

func countRows(ctx context.Context, database *sql.DB, table string) int {
	if !tableExists(ctx, database, table) {
		return 0
	}
	var count int
	// Table names are selected from constants in Import, never user input.
	if err := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&count); err != nil {
		return 0
	}
	return count
}

func parseLegacyTime(raw string, location *time.Location) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	formats := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}
	for _, format := range formats {
		if parsed, err := time.ParseInLocation(format, raw, location); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("unsupported timestamp %q", raw)
}

func parseStringList(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) == nil {
		return values
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	values = values[:0]
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			values = append(values, value)
		}
	}
	return values
}

func validFullName(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func normalizeGitHubStatus(value string, archived bool) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "active", "archived", "deleted", "private", "unreachable":
		return value
	case "not_found":
		return "unreachable"
	}
	if archived {
		return "archived"
	}
	return "active"
}

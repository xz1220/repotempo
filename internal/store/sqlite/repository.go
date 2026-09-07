package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const repositoryColumns = `
github_repo_id, github_node_id, full_name, html_url, description,
primary_language, github_created_at, first_seen_at, first_seen_source,
first_seen_profile, discovery_sources_json, last_discovered_at,
monitoring_status, github_status, is_focus, manual_note, github_etag,
last_checked_at, previous_names_json, created_at, updated_at`

type scanner interface {
	Scan(...any) error
}

func scanRepository(row scanner) (domain.Repository, error) {
	var repository domain.Repository
	var githubCreatedAt sql.NullString
	var firstSeenAt string
	var sourcesJSON string
	var lastDiscoveredAt string
	var isFocus int
	var lastCheckedAt sql.NullString
	var previousNamesJSON string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&repository.GitHubRepoID,
		&repository.GitHubNodeID,
		&repository.FullName,
		&repository.HTMLURL,
		&repository.Description,
		&repository.PrimaryLanguage,
		&githubCreatedAt,
		&firstSeenAt,
		&repository.FirstSeenSource,
		&repository.FirstSeenProfile,
		&sourcesJSON,
		&lastDiscoveredAt,
		&repository.MonitoringStatus,
		&repository.GitHubStatus,
		&isFocus,
		&repository.ManualNote,
		&repository.GitHubETag,
		&lastCheckedAt,
		&previousNamesJSON,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.Repository{}, err
	}

	var err error
	repository.GitHubCreatedAt, err = parseNullableStoredTime(githubCreatedAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.FirstSeenAt, err = parseStoredTime(firstSeenAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.LastDiscoveredAt, err = parseStoredTime(lastDiscoveredAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.LastCheckedAt, err = parseNullableStoredTime(lastCheckedAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.CreatedAt, err = parseStoredTime(createdAt)
	if err != nil {
		return domain.Repository{}, err
	}
	repository.UpdatedAt, err = parseStoredTime(updatedAt)
	if err != nil {
		return domain.Repository{}, err
	}
	if err := json.Unmarshal([]byte(sourcesJSON), &repository.DiscoverySources); err != nil {
		return domain.Repository{}, fmt.Errorf("decode repository discovery sources: %w", err)
	}
	if err := json.Unmarshal([]byte(previousNamesJSON), &repository.PreviousNames); err != nil {
		return domain.Repository{}, fmt.Errorf("decode repository previous names: %w", err)
	}
	repository.IsFocus = isFocus != 0
	return repository, nil
}

func validateObservation(observation domain.RepositoryObservation) error {
	if observation.GitHubRepoID <= 0 {
		return fmt.Errorf("%w: GitHub repository ID must be positive", corestore.ErrInvalid)
	}
	parts := strings.Split(observation.FullName, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("%w: repository full name must be owner/name", corestore.ErrInvalid)
	}
	if !observation.Source.Valid() {
		return fmt.Errorf("%w: invalid discovery source %q", corestore.ErrInvalid, observation.Source)
	}
	if observation.MonitoringStatus != "" && !observation.MonitoringStatus.Valid() {
		return fmt.Errorf("%w: invalid monitoring status %q", corestore.ErrInvalid, observation.MonitoringStatus)
	}
	if observation.GitHubStatus != "" && !observation.GitHubStatus.Valid() {
		return fmt.Errorf("%w: invalid GitHub status %q", corestore.ErrInvalid, observation.GitHubStatus)
	}
	return nil
}

func (store *Store) UpsertRepository(ctx context.Context, observation domain.RepositoryObservation) (domain.Repository, bool, error) {
	if err := validateObservation(observation); err != nil {
		return domain.Repository{}, false, err
	}
	now := store.nowUTC()
	if observation.DiscoveredAt.IsZero() {
		observation.DiscoveredAt = now
	} else {
		observation.DiscoveredAt = observation.DiscoveredAt.UTC()
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Repository{}, false, fmt.Errorf("begin repository upsert: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existing, err := scanRepository(transaction.QueryRowContext(ctx,
		"SELECT "+repositoryColumns+" FROM repositories WHERE github_repo_id = ?",
		observation.GitHubRepoID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		if err := ensureFullNameIdentity(ctx, transaction, observation.FullName, observation.GitHubRepoID); err != nil {
			return domain.Repository{}, false, err
		}
		repository := repositoryFromObservation(observation, now)
		if err := insertRepository(ctx, transaction, repository); err != nil {
			return domain.Repository{}, false, err
		}
		if err := transaction.Commit(); err != nil {
			return domain.Repository{}, false, fmt.Errorf("commit repository insert: %w", err)
		}
		return repository, true, nil
	}
	if err != nil {
		return domain.Repository{}, false, fmt.Errorf("read repository for upsert: %w", err)
	}
	if shouldAdoptObservedName(existing, observation) {
		if err := ensureFullNameIdentity(ctx, transaction, observation.FullName, observation.GitHubRepoID); err != nil {
			return domain.Repository{}, false, err
		}
	}

	merged := mergeRepositoryObservation(existing, observation, now)
	if err := updateRepository(ctx, transaction, merged); err != nil {
		return domain.Repository{}, false, err
	}
	if err := transaction.Commit(); err != nil {
		return domain.Repository{}, false, fmt.Errorf("commit repository update: %w", err)
	}
	return merged, false, nil
}

func shouldAdoptObservedName(existing domain.Repository, observation domain.RepositoryObservation) bool {
	return observation.FullName == existing.FullName || !observation.DiscoveredAt.Before(existing.LastDiscoveredAt)
}

func ensureFullNameIdentity(ctx context.Context, transaction *sql.Tx, fullName string, repositoryID int64) error {
	var conflictingID int64
	err := transaction.QueryRowContext(ctx, `
SELECT github_repo_id
FROM repositories
WHERE full_name = ? COLLATE NOCASE AND github_repo_id <> ?
LIMIT 1`, fullName, repositoryID).Scan(&conflictingID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check repository full-name identity: %w", err)
	}
	return fmt.Errorf("%w: %s is already associated with GitHub repository ID %d, not %d",
		corestore.ErrRepositoryIdentityMismatch, fullName, conflictingID, repositoryID)
}

func repositoryFromObservation(observation domain.RepositoryObservation, now time.Time) domain.Repository {
	monitoringStatus := observation.MonitoringStatus
	if monitoringStatus == "" {
		monitoringStatus = domain.MonitoringActive
	}
	githubStatus := observation.GitHubStatus
	if githubStatus == "" {
		githubStatus = domain.GitHubActive
	}
	repository := domain.Repository{
		GitHubRepoID:     observation.GitHubRepoID,
		GitHubNodeID:     observation.GitHubNodeID,
		FullName:         observation.FullName,
		HTMLURL:          observation.HTMLURL,
		FirstSeenAt:      observation.DiscoveredAt,
		FirstSeenSource:  observation.Source,
		FirstSeenProfile: observation.Profile,
		DiscoverySources: []domain.DiscoverySource{observation.Source},
		LastDiscoveredAt: observation.DiscoveredAt,
		MonitoringStatus: monitoringStatus,
		GitHubStatus:     githubStatus,
		PreviousNames:    []string{},
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if observation.Description != nil {
		repository.Description = *observation.Description
	}
	if observation.PrimaryLanguage != nil {
		repository.PrimaryLanguage = *observation.PrimaryLanguage
	}
	if observation.GitHubCreatedAt != nil {
		value := observation.GitHubCreatedAt.UTC()
		repository.GitHubCreatedAt = &value
	}
	if observation.IsFocus != nil {
		repository.IsFocus = *observation.IsFocus
	}
	if observation.ManualNote != nil {
		repository.ManualNote = *observation.ManualNote
	}
	if observation.GitHubETag != nil {
		repository.GitHubETag = *observation.GitHubETag
	}
	if observation.LastCheckedAt != nil {
		value := observation.LastCheckedAt.UTC()
		repository.LastCheckedAt = &value
	}
	return repository
}

func mergeRepositoryObservation(existing domain.Repository, observation domain.RepositoryObservation, now time.Time) domain.Repository {
	merged := existing
	isWatchlistReplay := observation.Source == domain.DiscoverySourceManual &&
		observation.Profile == "config-watchlist"
	isLatestObservation := !observation.DiscoveredAt.Before(existing.LastDiscoveredAt)
	if observation.GitHubNodeID != "" && (isLatestObservation || merged.GitHubNodeID == "") {
		merged.GitHubNodeID = observation.GitHubNodeID
	}
	if observation.FullName != existing.FullName {
		nameToRemember := observation.FullName
		if shouldAdoptObservedName(existing, observation) {
			nameToRemember = existing.FullName
			merged.FullName = observation.FullName
			merged.PreviousNames = slices.DeleteFunc(merged.PreviousNames, func(name string) bool {
				return name == observation.FullName
			})
		}
		if nameToRemember != "" && !slices.Contains(merged.PreviousNames, nameToRemember) {
			merged.PreviousNames = append(merged.PreviousNames, nameToRemember)
			if len(merged.PreviousNames) > 20 {
				merged.PreviousNames = merged.PreviousNames[len(merged.PreviousNames)-20:]
			}
		}
	}
	if observation.HTMLURL != "" && (isLatestObservation || merged.HTMLURL == "") {
		merged.HTMLURL = observation.HTMLURL
	}
	if observation.Description != nil && (isLatestObservation || merged.Description == "") {
		merged.Description = *observation.Description
	}
	if observation.PrimaryLanguage != nil && (isLatestObservation || merged.PrimaryLanguage == "") {
		merged.PrimaryLanguage = *observation.PrimaryLanguage
	}
	if observation.GitHubCreatedAt != nil && (isLatestObservation || merged.GitHubCreatedAt == nil) {
		value := observation.GitHubCreatedAt.UTC()
		merged.GitHubCreatedAt = &value
	}
	if observation.DiscoveredAt.Before(merged.FirstSeenAt) {
		merged.FirstSeenAt = observation.DiscoveredAt
		merged.FirstSeenSource = observation.Source
		merged.FirstSeenProfile = observation.Profile
	}
	if !slices.Contains(merged.DiscoverySources, observation.Source) {
		merged.DiscoverySources = append(merged.DiscoverySources, observation.Source)
	}
	if observation.DiscoveredAt.After(merged.LastDiscoveredAt) {
		merged.LastDiscoveredAt = observation.DiscoveredAt
	}
	if observation.MonitoringStatus != "" {
		if !isWatchlistReplay && (observation.Source == domain.DiscoverySourceManual ||
			monitoringSeverity(observation.MonitoringStatus) >= monitoringSeverity(merged.MonitoringStatus)) {
			merged.MonitoringStatus = observation.MonitoringStatus
		}
	}
	if observation.GitHubStatus != "" && isLatestObservation {
		merged.GitHubStatus = observation.GitHubStatus
	}
	if observation.IsFocus != nil {
		if !isWatchlistReplay && (observation.Source == domain.DiscoverySourceManual || *observation.IsFocus) {
			merged.IsFocus = *observation.IsFocus
		}
	}
	if observation.ManualNote != nil {
		if !isWatchlistReplay && (observation.Source == domain.DiscoverySourceManual || *observation.ManualNote != "") {
			merged.ManualNote = *observation.ManualNote
		}
	}
	if observation.GitHubETag != nil && isLatestObservation {
		merged.GitHubETag = *observation.GitHubETag
	}
	if observation.LastCheckedAt != nil {
		value := observation.LastCheckedAt.UTC()
		if merged.LastCheckedAt == nil || value.After(*merged.LastCheckedAt) {
			merged.LastCheckedAt = &value
		}
	}
	merged.UpdatedAt = now
	return merged
}

func monitoringSeverity(status domain.MonitoringStatus) int {
	switch status {
	case domain.MonitoringStopped:
		return 2
	case domain.MonitoringPaused:
		return 1
	default:
		return 0
	}
}

func insertRepository(ctx context.Context, transaction *sql.Tx, repository domain.Repository) error {
	sourcesJSON, err := json.Marshal(repository.DiscoverySources)
	if err != nil {
		return fmt.Errorf("encode repository discovery sources: %w", err)
	}
	previousNamesJSON, err := json.Marshal(repository.PreviousNames)
	if err != nil {
		return fmt.Errorf("encode repository previous names: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
INSERT INTO repositories (`+repositoryColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		repository.GitHubRepoID,
		repository.GitHubNodeID,
		repository.FullName,
		repository.HTMLURL,
		repository.Description,
		repository.PrimaryLanguage,
		nullableTime(repository.GitHubCreatedAt),
		storedTime(repository.FirstSeenAt),
		repository.FirstSeenSource,
		repository.FirstSeenProfile,
		string(sourcesJSON),
		storedTime(repository.LastDiscoveredAt),
		repository.MonitoringStatus,
		repository.GitHubStatus,
		boolInteger(repository.IsFocus),
		repository.ManualNote,
		repository.GitHubETag,
		nullableTime(repository.LastCheckedAt),
		string(previousNamesJSON),
		storedTime(repository.CreatedAt),
		storedTime(repository.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert repository: %w", err)
	}
	return nil
}

func updateRepository(ctx context.Context, transaction *sql.Tx, repository domain.Repository) error {
	sourcesJSON, err := json.Marshal(repository.DiscoverySources)
	if err != nil {
		return fmt.Errorf("encode repository discovery sources: %w", err)
	}
	previousNamesJSON, err := json.Marshal(repository.PreviousNames)
	if err != nil {
		return fmt.Errorf("encode repository previous names: %w", err)
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE repositories SET
    github_node_id = ?, full_name = ?, html_url = ?, description = ?,
    primary_language = ?, github_created_at = ?, first_seen_at = ?,
    first_seen_source = ?, first_seen_profile = ?, discovery_sources_json = ?,
    last_discovered_at = ?, monitoring_status = ?, github_status = ?,
    is_focus = ?, manual_note = ?, github_etag = ?, last_checked_at = ?,
    previous_names_json = ?, updated_at = ?
WHERE github_repo_id = ?`,
		repository.GitHubNodeID,
		repository.FullName,
		repository.HTMLURL,
		repository.Description,
		repository.PrimaryLanguage,
		nullableTime(repository.GitHubCreatedAt),
		storedTime(repository.FirstSeenAt),
		repository.FirstSeenSource,
		repository.FirstSeenProfile,
		string(sourcesJSON),
		storedTime(repository.LastDiscoveredAt),
		repository.MonitoringStatus,
		repository.GitHubStatus,
		boolInteger(repository.IsFocus),
		repository.ManualNote,
		repository.GitHubETag,
		nullableTime(repository.LastCheckedAt),
		string(previousNamesJSON),
		storedTime(repository.UpdatedAt),
		repository.GitHubRepoID,
	)
	if err != nil {
		return fmt.Errorf("update repository: %w", err)
	}
	return nil
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return storedTime(*value)
}

func (store *Store) GetRepository(ctx context.Context, repositoryID int64) (domain.Repository, error) {
	repository, err := scanRepository(store.db.QueryRowContext(ctx,
		"SELECT "+repositoryColumns+" FROM repositories WHERE github_repo_id = ?",
		repositoryID,
	))
	if err != nil {
		return domain.Repository{}, mapNotFound(err)
	}
	return repository, nil
}

func (store *Store) GetRepositoryByFullName(ctx context.Context, fullName string) (domain.Repository, error) {
	repository, err := scanRepository(store.db.QueryRowContext(ctx,
		"SELECT "+repositoryColumns+" FROM repositories WHERE full_name = ? COLLATE NOCASE ORDER BY updated_at DESC LIMIT 1",
		fullName,
	))
	if err != nil {
		return domain.Repository{}, mapNotFound(err)
	}
	return repository, nil
}

func (store *Store) ListRepositories(ctx context.Context, filter domain.RepositoryFilter) ([]domain.Repository, error) {
	query := "SELECT " + repositoryColumns + " FROM repositories WHERE 1 = 1"
	arguments := make([]any, 0, 8)
	if filter.MonitoringStatus != "" {
		if !filter.MonitoringStatus.Valid() {
			return nil, fmt.Errorf("%w: invalid monitoring status %q", corestore.ErrInvalid, filter.MonitoringStatus)
		}
		query += " AND monitoring_status = ?"
		arguments = append(arguments, filter.MonitoringStatus)
	}
	if filter.GitHubStatus != "" {
		if !filter.GitHubStatus.Valid() {
			return nil, fmt.Errorf("%w: invalid GitHub status %q", corestore.ErrInvalid, filter.GitHubStatus)
		}
		query += " AND github_status = ?"
		arguments = append(arguments, filter.GitHubStatus)
	}
	if filter.DiscoverySource != "" {
		if !filter.DiscoverySource.Valid() {
			return nil, fmt.Errorf("%w: invalid discovery source %q", corestore.ErrInvalid, filter.DiscoverySource)
		}
		query += " AND EXISTS (SELECT 1 FROM json_each(repositories.discovery_sources_json) WHERE value = ?)"
		arguments = append(arguments, filter.DiscoverySource)
	}
	if filter.FocusOnly {
		query += " AND is_focus = 1"
	}
	query += " ORDER BY full_name COLLATE NOCASE"
	limit, offset, err := normalizePage(filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	query += " LIMIT ? OFFSET ?"
	arguments = append(arguments, limit, offset)

	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list repositories: %w", err)
	}
	defer rows.Close()
	repositories := make([]domain.Repository, 0)
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository: %w", err)
		}
		repositories = append(repositories, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repositories: %w", err)
	}
	return repositories, nil
}

func normalizePage(limit, offset int) (int, int, error) {
	if limit < 0 || offset < 0 {
		return 0, 0, fmt.Errorf("%w: pagination values cannot be negative", corestore.ErrInvalid)
	}
	if limit == 0 {
		limit = -1
	}
	return limit, offset, nil
}

func (store *Store) SetRepositoryMonitoringStatus(ctx context.Context, repositoryID int64, status domain.MonitoringStatus) error {
	if !status.Valid() {
		return fmt.Errorf("%w: invalid monitoring status %q", corestore.ErrInvalid, status)
	}
	return store.updateRepositoryStatus(ctx, repositoryID, "monitoring_status", status)
}

func (store *Store) SetRepositoryGitHubStatus(ctx context.Context, repositoryID int64, status domain.GitHubStatus) error {
	if !status.Valid() {
		return fmt.Errorf("%w: invalid GitHub status %q", corestore.ErrInvalid, status)
	}
	return store.updateRepositoryStatus(ctx, repositoryID, "github_status", status)
}

func (store *Store) updateRepositoryStatus(ctx context.Context, repositoryID int64, column string, status any) error {
	result, err := store.db.ExecContext(ctx,
		"UPDATE repositories SET "+column+" = ?, updated_at = ? WHERE github_repo_id = ?",
		status, storedTime(store.nowUTC()), repositoryID,
	)
	if err != nil {
		return fmt.Errorf("update repository status: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read repository status update result: %w", err)
	}
	if affected == 0 {
		return corestore.ErrNotFound
	}
	return nil
}

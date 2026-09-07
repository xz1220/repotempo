package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
)

// PutWatchedRepository makes a manual add atomic. Existing notes and topic
// assignments are retained; an empty note can be filled and a selected topic
// is added without replacing any prior classification.
func (store *Store) PutWatchedRepository(ctx context.Context, observation domain.RepositoryObservation, snapshot domain.DailySnapshot, topicSlug string) (domain.Repository, bool, error) {
	if err := validateObservation(observation); err != nil {
		return domain.Repository{}, false, err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return domain.Repository{}, false, err
	}
	if observation.GitHubRepoID != snapshot.RepositoryID || snapshot.FetchStatus != domain.FetchSuccess {
		return domain.Repository{}, false, corestore.ErrInvalid
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Repository{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	now := store.nowUTC()
	var topicID int64
	if topicSlug != "" {
		if err := tx.QueryRowContext(ctx, "SELECT id FROM topics WHERE slug = ? COLLATE NOCASE AND status = 'active'", topicSlug).Scan(&topicID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.Repository{}, false, fmt.Errorf("%w: unknown topic", corestore.ErrInvalid)
			}
			return domain.Repository{}, false, err
		}
	}
	existing, err := scanRepository(tx.QueryRowContext(ctx, "SELECT "+repositoryColumns+" FROM repositories WHERE github_repo_id = ?", observation.GitHubRepoID))
	created := errors.Is(err, sql.ErrNoRows)
	if err != nil && !created {
		return domain.Repository{}, false, err
	}
	if err := ensureFullNameIdentity(ctx, tx, observation.FullName, observation.GitHubRepoID); err != nil {
		return domain.Repository{}, false, err
	}
	var repository domain.Repository
	if created {
		repository = repositoryFromObservation(observation, now)
		err = insertRepository(ctx, tx, repository)
	} else {
		if existing.ManualNote != "" {
			observation.ManualNote = nil
		}
		repository = mergeRepositoryObservation(existing, observation, now)
		err = updateRepository(ctx, tx, repository)
	}
	if err != nil {
		return domain.Repository{}, false, err
	}
	snapshot.CreatedAt = now
	prior, err := scanSnapshot(tx.QueryRowContext(ctx, "SELECT "+snapshotColumns+" FROM daily_snapshots WHERE repository_id = ? AND snapshot_date = ?", snapshot.RepositoryID, snapshot.SnapshotDate))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		err = insertSnapshot(ctx, tx, snapshot)
	case err != nil:
		return domain.Repository{}, false, err
	case prior.FetchStatus == domain.FetchFailed:
		snapshot.OSSTodayRank, snapshot.OSSWindowStars, snapshot.OSSTotalScore = prior.OSSTodayRank, prior.OSSWindowStars, prior.OSSTotalScore
		err = repairSnapshot(ctx, tx, snapshot)
	}
	if err != nil {
		return domain.Repository{}, false, err
	}
	if topicID > 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO repository_topics
			(repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at)
			VALUES (?, ?, 'manual', 1, NULL, ?, ?) ON CONFLICT(repository_id, topic_id) DO NOTHING`,
			repository.GitHubRepoID, topicID, storedTime(now), storedTime(now))
		if err != nil {
			return domain.Repository{}, false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Repository{}, false, err
	}
	return repository, created, nil
}

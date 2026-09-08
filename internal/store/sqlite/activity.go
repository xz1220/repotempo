package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

// prepareRepositoryActivity validates evidence without filling unknown values
// or inferring commits from generated text, repository dates, or Star counts.
func prepareRepositoryActivity(repositoryID int64, activity domain.RepositoryActivity) (domain.RepositoryActivity, error) {
	invalid := func(reason string) (domain.RepositoryActivity, error) {
		return domain.RepositoryActivity{}, fmt.Errorf("%w: repository activity: %s", corestore.ErrInvalid, reason)
	}
	if repositoryID <= 0 || activity.RepositoryID != repositoryID {
		return invalid("positive repository ID must match the target")
	}
	if activity.DefaultBranch == "" || strings.TrimSpace(activity.DefaultBranch) != activity.DefaultBranch ||
		len(activity.DefaultBranch) > 1024 || !utf8.ValidString(activity.DefaultBranch) || strings.ContainsFunc(activity.DefaultBranch, unicode.IsControl) {
		return invalid("default branch must be a nonempty valid string")
	}
	if activity.HeadSHA != "" {
		if len(activity.HeadSHA) != 40 && len(activity.HeadSHA) != 64 {
			return invalid("head SHA must be a full hexadecimal object ID")
		}
		if _, err := hex.DecodeString(activity.HeadSHA); err != nil {
			return invalid("head SHA must be a full hexadecimal object ID")
		}
	}
	for _, value := range []time.Time{activity.WindowStart, activity.WindowEnd, activity.FetchedAt} {
		if value.IsZero() || value.Year() < 1 || value.Year() > 9999 {
			return invalid("window and fetch timestamps are required")
		}
	}
	activity.WindowStart = activity.WindowStart.UTC()
	activity.WindowEnd = activity.WindowEnd.UTC()
	activity.FetchedAt = activity.FetchedAt.UTC()
	endDate := time.Date(activity.WindowEnd.Year(), activity.WindowEnd.Month(), activity.WindowEnd.Day(), 0, 0, 0, 0, time.UTC)
	if !activity.WindowStart.Equal(endDate.AddDate(0, 0, -29)) || activity.WindowStart.Year() < 1 {
		return invalid("window must contain the last 30 UTC dates, including the partial end date")
	}
	if activity.FetchedAt.Before(activity.WindowEnd) {
		return invalid("fetch timestamp cannot precede the window end")
	}
	if activity.Commits < 0 || activity.Commits > 500 || activity.ActiveDays < 0 || activity.ActiveDays > 30 {
		return invalid("commit and active-day counts exceed their bounds")
	}
	if !activity.Complete && activity.Commits == 0 {
		return invalid("an incomplete cache must contain observed commits")
	}
	if len(activity.Daily) != 30 {
		return invalid("daily activity must contain all 30 UTC dates")
	}
	commits, activeDays := 0, 0
	for index, day := range activity.Daily {
		if day.Date != activity.WindowStart.AddDate(0, 0, index).Format(time.DateOnly) || day.Count < 0 || day.Count > 500 {
			return invalid("daily activity must have ordered, unique, in-window dates and nonnegative bounded counts")
		}
		commits += day.Count
		if day.Count > 0 {
			activeDays++
		}
	}
	if commits != activity.Commits || activeDays != activity.ActiveDays {
		return invalid("daily counts must match total commits and active days")
	}
	if activity.LatestCommitAt != nil {
		latest := activity.LatestCommitAt.UTC()
		if latest.IsZero() || latest.Year() < 1 || latest.Year() > 9999 || latest.After(activity.WindowEnd) {
			return invalid("latest commit timestamp must be valid and no later than the window end")
		}
		if activity.Commits == 0 && !latest.Before(activity.WindowStart) {
			return invalid("an in-window latest commit cannot have an observed zero total")
		}
		// Commit graph order is not committer timestamp order. The branch's
		// newest commit may be dated before a parent, even before this window.
		activity.LatestCommitAt = &latest
	}
	if activity.Commits > 0 && activity.LatestCommitAt == nil {
		return invalid("observed commits require a latest commit timestamp")
	}
	if activity.HeadSHA == "" && (activity.Commits != 0 || activity.LatestCommitAt != nil) {
		return invalid("observed commit history requires a head SHA")
	}
	return activity, nil
}

func decodeRepositoryActivity(repositoryID int64, encoded string) (domain.RepositoryActivity, error) {
	var activity domain.RepositoryActivity
	if err := json.Unmarshal([]byte(encoded), &activity); err != nil {
		return domain.RepositoryActivity{}, fmt.Errorf("decode repository activity: %w", err)
	}
	return prepareRepositoryActivity(repositoryID, activity)
}

// PutRepositoryActivity updates only the activity cache of an existing stable
// GitHub ID. Metadata refreshes and activity collection cannot clear each other.
// Older/equal fetches are idempotent no-ops; the first result at a fetch instant
// wins, including when another process has already stored a newer result.
func (store *Store) PutRepositoryActivity(ctx context.Context, repositoryID int64, activity domain.RepositoryActivity) error {
	activity, err := prepareRepositoryActivity(repositoryID, activity)
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("encode repository activity: %w", err)
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin activity update: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	var previous sql.NullString
	if err := transaction.QueryRowContext(ctx, "SELECT activity_json FROM repositories WHERE github_repo_id = ?", repositoryID).Scan(&previous); err != nil {
		return fmt.Errorf("read repository activity for update: %w", mapNotFound(err))
	}
	if previous.Valid {
		cached, err := decodeRepositoryActivity(repositoryID, previous.String)
		if err != nil {
			return err
		}
		if !activity.FetchedAt.After(cached.FetchedAt) {
			return nil
		}
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE repositories SET activity_json = ? WHERE github_repo_id = ?", string(encoded), repositoryID); err != nil {
		return fmt.Errorf("update repository activity: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit repository activity: %w", err)
	}
	return nil
}

// ActivityCandidates selects a bounded queue of public monitored repositories.
// First-seen bounds are inclusive since / exclusive until instants. Freshness
// is independent of the chosen first-seen cohort, and NULL means never fetched.
func (store *Store) ActivityCandidates(ctx context.Context, since, until *time.Time, staleBefore time.Time, limit int) ([]domain.Repository, error) {
	if limit < 1 || limit > 500 || staleBefore.IsZero() ||
		(since != nil && since.IsZero()) || (until != nil && until.IsZero()) ||
		(since != nil && until != nil && !since.Before(*until)) {
		return nil, fmt.Errorf("%w: activity candidate range, freshness cutoff and limit 1..500 are required", corestore.ErrInvalid)
	}
	query := "SELECT " + repositoryColumns + ` FROM repositories
WHERE monitoring_status = 'active' AND github_status IN ('active', 'archived')
AND (activity_json IS NULL OR julianday(json_extract(activity_json, '$.fetched_at')) < julianday(?))`
	arguments := []any{storedTime(staleBefore)}
	if since != nil {
		query += " AND julianday(first_seen_at) >= julianday(?)"
		arguments = append(arguments, storedTime(*since))
	}
	if until != nil {
		query += " AND julianday(first_seen_at) < julianday(?)"
		arguments = append(arguments, storedTime(*until))
	}
	query += " ORDER BY is_focus DESC, (activity_json IS NULL) DESC, first_seen_at DESC, github_repo_id DESC LIMIT ?"
	arguments = append(arguments, limit)
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("query activity candidates: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Repository, 0)
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, fmt.Errorf("scan activity candidate: %w", err)
		}
		result = append(result, repository)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate activity candidates: %w", err)
	}
	return result, nil
}

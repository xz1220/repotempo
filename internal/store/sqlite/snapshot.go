package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const snapshotColumns = `
repository_id, snapshot_date, captured_at, star_count, fetch_status,
http_status, error_code, oss_today_rank, oss_window_stars,
oss_total_score, created_at`

func validateSnapshot(snapshot domain.DailySnapshot) error {
	if snapshot.RepositoryID <= 0 {
		return fmt.Errorf("%w: repository ID must be positive", corestore.ErrInvalid)
	}
	if err := snapshot.SnapshotDate.Validate(); err != nil {
		return fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	if !snapshot.FetchStatus.Valid() {
		return fmt.Errorf("%w: invalid fetch status %q", corestore.ErrInvalid, snapshot.FetchStatus)
	}
	if snapshot.FetchStatus == domain.FetchSuccess && snapshot.StarCount == nil {
		return fmt.Errorf("%w: successful snapshot requires a star count", corestore.ErrInvalid)
	}
	if snapshot.FetchStatus == domain.FetchFailed && snapshot.StarCount != nil {
		return fmt.Errorf("%w: failed snapshot star count must be NULL", corestore.ErrInvalid)
	}
	if snapshot.StarCount != nil && *snapshot.StarCount < 0 {
		return fmt.Errorf("%w: star count cannot be negative", corestore.ErrInvalid)
	}
	if snapshot.HTTPStatus != nil && (*snapshot.HTTPStatus < 100 || *snapshot.HTTPStatus > 599) {
		return fmt.Errorf("%w: HTTP status must be between 100 and 599", corestore.ErrInvalid)
	}
	if snapshot.OSSTodayRank != nil && *snapshot.OSSTodayRank <= 0 {
		return fmt.Errorf("%w: OSS Insight rank must be positive", corestore.ErrInvalid)
	}
	if snapshot.OSSWindowStars != nil && *snapshot.OSSWindowStars < 0 {
		return fmt.Errorf("%w: OSS Insight window stars cannot be negative", corestore.ErrInvalid)
	}
	return nil
}

func scanSnapshot(row scanner) (domain.DailySnapshot, error) {
	var snapshot domain.DailySnapshot
	var snapshotDate string
	var capturedAt string
	var starCount sql.NullInt64
	var httpStatus sql.NullInt64
	var ossTodayRank sql.NullInt64
	var ossWindowStars sql.NullInt64
	var ossTotalScore sql.NullFloat64
	var createdAt string
	if err := row.Scan(
		&snapshot.RepositoryID,
		&snapshotDate,
		&capturedAt,
		&starCount,
		&snapshot.FetchStatus,
		&httpStatus,
		&snapshot.ErrorCode,
		&ossTodayRank,
		&ossWindowStars,
		&ossTotalScore,
		&createdAt,
	); err != nil {
		return domain.DailySnapshot{}, err
	}
	date, err := domain.ParseDate(snapshotDate)
	if err != nil {
		return domain.DailySnapshot{}, err
	}
	snapshot.SnapshotDate = date
	snapshot.CapturedAt, err = parseStoredTime(capturedAt)
	if err != nil {
		return domain.DailySnapshot{}, err
	}
	snapshot.CreatedAt, err = parseStoredTime(createdAt)
	if err != nil {
		return domain.DailySnapshot{}, err
	}
	if starCount.Valid {
		value := starCount.Int64
		snapshot.StarCount = &value
	}
	if httpStatus.Valid {
		value := int(httpStatus.Int64)
		snapshot.HTTPStatus = &value
	}
	if ossTodayRank.Valid {
		value := int(ossTodayRank.Int64)
		snapshot.OSSTodayRank = &value
	}
	if ossWindowStars.Valid {
		value := ossWindowStars.Int64
		snapshot.OSSWindowStars = &value
	}
	if ossTotalScore.Valid {
		value := ossTotalScore.Float64
		snapshot.OSSTotalScore = &value
	}
	return snapshot, nil
}

func (store *Store) PutDailySnapshot(ctx context.Context, snapshot domain.DailySnapshot) (domain.SnapshotWriteResult, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return domain.SnapshotWriteResult{}, err
	}
	if snapshot.CapturedAt.IsZero() {
		snapshot.CapturedAt = store.nowUTC()
	} else {
		snapshot.CapturedAt = snapshot.CapturedAt.UTC()
	}

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.SnapshotWriteResult{}, fmt.Errorf("begin snapshot write: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	existing, err := scanSnapshot(transaction.QueryRowContext(ctx,
		"SELECT "+snapshotColumns+" FROM daily_snapshots WHERE repository_id = ? AND snapshot_date = ?",
		snapshot.RepositoryID,
		snapshot.SnapshotDate,
	))
	if errors.Is(err, sql.ErrNoRows) {
		snapshot.CreatedAt = store.nowUTC()
		if err := insertSnapshot(ctx, transaction, snapshot); err != nil {
			return domain.SnapshotWriteResult{}, err
		}
		if err := transaction.Commit(); err != nil {
			return domain.SnapshotWriteResult{}, fmt.Errorf("commit snapshot insert: %w", err)
		}
		return domain.SnapshotWriteResult{Disposition: domain.SnapshotInserted, Snapshot: snapshot}, nil
	}
	if err != nil {
		return domain.SnapshotWriteResult{}, fmt.Errorf("read existing snapshot: %w", err)
	}
	if existing.FetchStatus == domain.FetchSuccess {
		if err := transaction.Commit(); err != nil {
			return domain.SnapshotWriteResult{}, fmt.Errorf("commit protected snapshot read: %w", err)
		}
		return domain.SnapshotWriteResult{Disposition: domain.SnapshotSuccessProtected, Snapshot: existing}, nil
	}
	if snapshot.FetchStatus != domain.FetchSuccess {
		if err := transaction.Commit(); err != nil {
			return domain.SnapshotWriteResult{}, fmt.Errorf("commit unchanged snapshot read: %w", err)
		}
		return domain.SnapshotWriteResult{Disposition: domain.SnapshotUnchanged, Snapshot: existing}, nil
	}

	// The successful retry repairs only the failed outcome. OSS Insight fields
	// attached to the original daily row survive when the retry did not carry
	// them again.
	snapshot.CreatedAt = existing.CreatedAt
	if snapshot.OSSTodayRank == nil {
		snapshot.OSSTodayRank = existing.OSSTodayRank
	}
	if snapshot.OSSWindowStars == nil {
		snapshot.OSSWindowStars = existing.OSSWindowStars
	}
	if snapshot.OSSTotalScore == nil {
		snapshot.OSSTotalScore = existing.OSSTotalScore
	}
	if err := repairSnapshot(ctx, transaction, snapshot); err != nil {
		return domain.SnapshotWriteResult{}, err
	}
	if err := transaction.Commit(); err != nil {
		return domain.SnapshotWriteResult{}, fmt.Errorf("commit snapshot repair: %w", err)
	}
	return domain.SnapshotWriteResult{Disposition: domain.SnapshotFailureRepaired, Snapshot: snapshot}, nil
}

func insertSnapshot(ctx context.Context, transaction *sql.Tx, snapshot domain.DailySnapshot) error {
	_, err := transaction.ExecContext(ctx, `
INSERT INTO daily_snapshots (`+snapshotColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		snapshot.RepositoryID,
		snapshot.SnapshotDate,
		storedTime(snapshot.CapturedAt),
		nullableInt64(snapshot.StarCount),
		snapshot.FetchStatus,
		nullableInt(snapshot.HTTPStatus),
		snapshot.ErrorCode,
		nullableInt(snapshot.OSSTodayRank),
		nullableInt64(snapshot.OSSWindowStars),
		nullableFloat64(snapshot.OSSTotalScore),
		storedTime(snapshot.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("insert daily snapshot: %w", err)
	}
	return nil
}

func repairSnapshot(ctx context.Context, transaction *sql.Tx, snapshot domain.DailySnapshot) error {
	result, err := transaction.ExecContext(ctx, `
UPDATE daily_snapshots SET
    captured_at = ?, star_count = ?, fetch_status = ?, http_status = ?,
    error_code = ?, oss_today_rank = ?, oss_window_stars = ?, oss_total_score = ?
WHERE repository_id = ? AND snapshot_date = ? AND fetch_status = 'failed'`,
		storedTime(snapshot.CapturedAt),
		nullableInt64(snapshot.StarCount),
		snapshot.FetchStatus,
		nullableInt(snapshot.HTTPStatus),
		snapshot.ErrorCode,
		nullableInt(snapshot.OSSTodayRank),
		nullableInt64(snapshot.OSSWindowStars),
		nullableFloat64(snapshot.OSSTotalScore),
		snapshot.RepositoryID,
		snapshot.SnapshotDate,
	)
	if err != nil {
		return fmt.Errorf("repair daily snapshot: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read snapshot repair result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("repair daily snapshot: expected one failed row, updated %d", affected)
	}
	return nil
}

func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloat64(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func (store *Store) GetDailySnapshot(ctx context.Context, repositoryID int64, date domain.Date) (domain.DailySnapshot, error) {
	if err := date.Validate(); err != nil {
		return domain.DailySnapshot{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	snapshot, err := scanSnapshot(store.db.QueryRowContext(ctx,
		"SELECT "+snapshotColumns+" FROM daily_snapshots WHERE repository_id = ? AND snapshot_date = ?",
		repositoryID,
		date,
	))
	if err != nil {
		return domain.DailySnapshot{}, mapNotFound(err)
	}
	return snapshot, nil
}

func (store *Store) GetLatestSuccessfulSnapshot(ctx context.Context, repositoryID int64, beforeOrOn domain.Date) (domain.DailySnapshot, error) {
	if err := beforeOrOn.Validate(); err != nil {
		return domain.DailySnapshot{}, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
	}
	snapshot, err := scanSnapshot(store.db.QueryRowContext(ctx, `
SELECT `+snapshotColumns+`
FROM daily_snapshots
WHERE repository_id = ? AND snapshot_date <= ? AND fetch_status = 'success'
ORDER BY snapshot_date DESC
LIMIT 1`, repositoryID, beforeOrOn))
	if err != nil {
		return domain.DailySnapshot{}, mapNotFound(err)
	}
	return snapshot, nil
}

func (store *Store) ListDailySnapshots(ctx context.Context, repositoryID int64, filter domain.SnapshotFilter) ([]domain.DailySnapshot, error) {
	query := "SELECT " + snapshotColumns + " FROM daily_snapshots WHERE repository_id = ?"
	arguments := []any{repositoryID}
	if filter.From != "" {
		if err := filter.From.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
		}
		query += " AND snapshot_date >= ?"
		arguments = append(arguments, filter.From)
	}
	if filter.Through != "" {
		if err := filter.Through.Validate(); err != nil {
			return nil, fmt.Errorf("%w: %v", corestore.ErrInvalid, err)
		}
		query += " AND snapshot_date <= ?"
		arguments = append(arguments, filter.Through)
	}
	if filter.FailuresOnly {
		query += " AND fetch_status = 'failed'"
	}
	query += " ORDER BY snapshot_date ASC"
	limit, offset, err := normalizePage(filter.Limit, filter.Offset)
	if err != nil {
		return nil, err
	}
	query += " LIMIT ? OFFSET ?"
	arguments = append(arguments, limit, offset)

	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list daily snapshots: %w", err)
	}
	defer rows.Close()
	snapshots := make([]domain.DailySnapshot, 0)
	for rows.Next() {
		snapshot, err := scanSnapshot(rows)
		if err != nil {
			return nil, fmt.Errorf("scan daily snapshot: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily snapshots: %w", err)
	}
	return snapshots, nil
}

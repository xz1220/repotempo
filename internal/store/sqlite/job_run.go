package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const jobRunColumns = `
run_id, job_type, started_at, finished_at, status, target_count,
success_count, failure_count, skipped_count, details_json, error_summary,
created_at`

func normalizeJobRun(run domain.JobRun, nowIfMissing bool, now time.Time) (domain.JobRun, error) {
	if strings.TrimSpace(run.RunID) == "" || strings.TrimSpace(run.JobType) == "" {
		return domain.JobRun{}, fmt.Errorf("%w: job run ID and type are required", corestore.ErrInvalid)
	}
	if run.Status == "" {
		run.Status = domain.JobRunning
	}
	if !run.Status.Valid() {
		return domain.JobRun{}, fmt.Errorf("%w: invalid job status %q", corestore.ErrInvalid, run.Status)
	}
	if run.TargetCount < 0 || run.SuccessCount < 0 || run.FailureCount < 0 || run.SkippedCount < 0 {
		return domain.JobRun{}, fmt.Errorf("%w: job counts cannot be negative", corestore.ErrInvalid)
	}
	if len(run.Details) == 0 {
		run.Details = json.RawMessage(`{}`)
	}
	var details map[string]any
	if !json.Valid(run.Details) || json.Unmarshal(run.Details, &details) != nil || details == nil {
		return domain.JobRun{}, fmt.Errorf("%w: job details must be a JSON object", corestore.ErrInvalid)
	}
	if nowIfMissing && run.StartedAt.IsZero() {
		run.StartedAt = now
	}
	if run.StartedAt.IsZero() {
		return domain.JobRun{}, fmt.Errorf("%w: job start time is required", corestore.ErrInvalid)
	}
	run.StartedAt = run.StartedAt.UTC()
	if run.Status == domain.JobRunning {
		if run.FinishedAt != nil {
			return domain.JobRun{}, fmt.Errorf("%w: running job cannot have a finish time", corestore.ErrInvalid)
		}
	} else if run.FinishedAt == nil && nowIfMissing {
		finishedAt := now
		run.FinishedAt = &finishedAt
	}
	if run.FinishedAt != nil {
		value := run.FinishedAt.UTC()
		run.FinishedAt = &value
	}
	if nowIfMissing && run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if !run.CreatedAt.IsZero() {
		run.CreatedAt = run.CreatedAt.UTC()
	}
	return run, nil
}

func scanJobRun(row scanner) (domain.JobRun, error) {
	var run domain.JobRun
	var startedAt string
	var finishedAt sql.NullString
	var detailsJSON string
	var createdAt string
	if err := row.Scan(
		&run.RunID,
		&run.JobType,
		&startedAt,
		&finishedAt,
		&run.Status,
		&run.TargetCount,
		&run.SuccessCount,
		&run.FailureCount,
		&run.SkippedCount,
		&detailsJSON,
		&run.ErrorSummary,
		&createdAt,
	); err != nil {
		return domain.JobRun{}, err
	}
	var err error
	run.StartedAt, err = parseStoredTime(startedAt)
	if err != nil {
		return domain.JobRun{}, err
	}
	run.FinishedAt, err = parseNullableStoredTime(finishedAt)
	if err != nil {
		return domain.JobRun{}, err
	}
	run.CreatedAt, err = parseStoredTime(createdAt)
	if err != nil {
		return domain.JobRun{}, err
	}
	run.Details = json.RawMessage(detailsJSON)
	return run, nil
}

func (store *Store) CreateJobRun(ctx context.Context, run domain.JobRun) error {
	normalized, err := normalizeJobRun(run, true, store.nowUTC())
	if err != nil {
		return err
	}
	_, err = store.db.ExecContext(ctx, `
INSERT INTO job_runs (`+jobRunColumns+`)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		normalized.RunID,
		normalized.JobType,
		storedTime(normalized.StartedAt),
		nullableTime(normalized.FinishedAt),
		normalized.Status,
		normalized.TargetCount,
		normalized.SuccessCount,
		normalized.FailureCount,
		normalized.SkippedCount,
		string(normalized.Details),
		normalized.ErrorSummary,
		storedTime(normalized.CreatedAt),
	)
	if err != nil {
		return fmt.Errorf("create job run: %w", err)
	}
	return nil
}

func (store *Store) UpdateJobRun(ctx context.Context, run domain.JobRun) error {
	existing, err := store.GetJobRun(ctx, run.RunID)
	if err != nil {
		return err
	}
	if run.JobType == "" {
		run.JobType = existing.JobType
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = existing.StartedAt
	}
	if len(run.Details) == 0 {
		run.Details = existing.Details
	}
	run.CreatedAt = existing.CreatedAt
	normalized, err := normalizeJobRun(run, true, store.nowUTC())
	if err != nil {
		return err
	}
	result, err := store.db.ExecContext(ctx, `
UPDATE job_runs SET
    job_type = ?, started_at = ?, finished_at = ?, status = ?,
    target_count = ?, success_count = ?, failure_count = ?, skipped_count = ?,
    details_json = ?, error_summary = ?
WHERE run_id = ?`,
		normalized.JobType,
		storedTime(normalized.StartedAt),
		nullableTime(normalized.FinishedAt),
		normalized.Status,
		normalized.TargetCount,
		normalized.SuccessCount,
		normalized.FailureCount,
		normalized.SkippedCount,
		string(normalized.Details),
		normalized.ErrorSummary,
		normalized.RunID,
	)
	if err != nil {
		return fmt.Errorf("update job run: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read job update result: %w", err)
	}
	if affected == 0 {
		return corestore.ErrNotFound
	}
	return nil
}

func (store *Store) GetJobRun(ctx context.Context, runID string) (domain.JobRun, error) {
	run, err := scanJobRun(store.db.QueryRowContext(ctx,
		"SELECT "+jobRunColumns+" FROM job_runs WHERE run_id = ?", runID,
	))
	if err != nil {
		return domain.JobRun{}, mapNotFound(err)
	}
	return run, nil
}

func (store *Store) ListJobRuns(ctx context.Context, limit, offset int) ([]domain.JobRun, error) {
	limit, offset, err := normalizePage(limit, offset)
	if err != nil {
		return nil, err
	}
	rows, err := store.db.QueryContext(ctx,
		"SELECT "+jobRunColumns+" FROM job_runs ORDER BY started_at DESC LIMIT ? OFFSET ?",
		limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list job runs: %w", err)
	}
	defer rows.Close()
	runs := make([]domain.JobRun, 0)
	for rows.Next() {
		run, err := scanJobRun(rows)
		if err != nil {
			return nil, fmt.Errorf("scan job run: %w", err)
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate job runs: %w", err)
	}
	return runs, nil
}

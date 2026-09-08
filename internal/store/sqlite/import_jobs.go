package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

// Import state lives in its own job_runs details envelope. Generic run updates
// must not modify these records: lease changes require a compare-and-swap.
type importJobState struct {
	Version    int                        `json:"version"`
	Job        domain.RepositoryImportJob `json:"job"`
	LeaseToken string                     `json:"lease_token,omitempty"`
	LeaseUntil *time.Time                 `json:"lease_until,omitempty"`
}

var importName = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9_.-]{1,100}$`)

func validImportRequest(request domain.RepositoryImportRequest) bool {
	parts := strings.Split(request.Repository, "/")
	return importName.MatchString(request.Repository) && !strings.Contains(parts[0], "--") && parts[1] != "." && parts[1] != ".." &&
		utf8.ValidString(request.Note) && utf8.RuneCountInString(request.Note) <= 2000 && len(request.TopicSlug) <= 100
}

// Immediate transactions serialize queue capacity checks and claims across
// processes. No network request or parsing runs while this lock is held.
func (store *Store) importTransaction(ctx context.Context, operation func(*sql.Conn) error) error {
	connection, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(cleanup, "ROLLBACK")
	}()
	if err := operation(connection); err != nil {
		return err
	}
	_, err = connection.ExecContext(ctx, "COMMIT")
	return err
}

func decodeImportState(encoded []byte) (importJobState, error) {
	var state importJobState
	if len(encoded) > 128*1024 || json.Unmarshal(encoded, &state) != nil || state.Version != 1 || state.Job.ID == "" || !validImportRequest(state.Job.Request) || state.Job.Attempts < 0 || state.Job.Attempts > domain.ImportMaxAttempts {
		return importJobState{}, fmt.Errorf("%w: invalid saved import state", corestore.ErrInvalid)
	}
	switch state.Job.Stage {
	case "queued", "resolving", "reading", "classifying", "done", "partial", "failed":
	default:
		return importJobState{}, fmt.Errorf("%w: invalid saved import stage", corestore.ErrInvalid)
	}
	switch state.Job.ErrorCode {
	case "", "invalid", "not_found_or_private", "identity_mismatch", "rate_limited", "upstream_unavailable", "readme_unavailable", "readme_invalid", "classification_failed", "attempts_exhausted", "interrupted", "configuration_unavailable", "storage_unavailable":
	default:
		return importJobState{}, fmt.Errorf("%w: invalid saved import error code", corestore.ErrInvalid)
	}
	return state, nil
}

func pendingImports(ctx context.Context, connection *sql.Conn) ([]importJobState, error) {
	rows, err := connection.QueryContext(ctx, `SELECT details_json FROM job_runs WHERE job_type=? AND status='running' ORDER BY julianday(created_at),rowid`, domain.ImportJobType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []importJobState
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		state, err := decodeImportState(encoded)
		if err != nil {
			return nil, err
		}
		result = append(result, state)
	}
	return result, rows.Err()
}

func (store *Store) EnqueueRepositoryImport(ctx context.Context, request domain.RepositoryImportRequest) (domain.RepositoryImportJob, error) {
	request.Repository = strings.ToLower(strings.TrimSpace(request.Repository))
	request.Note, request.TopicSlug = strings.TrimSpace(request.Note), strings.TrimSpace(request.TopicSlug)
	if !validImportRequest(request) {
		return domain.RepositoryImportJob{}, domain.ErrImportInvalid
	}
	var result domain.RepositoryImportJob
	err := store.importTransaction(ctx, func(connection *sql.Conn) error {
		pending, err := pendingImports(ctx, connection)
		if err != nil {
			return err
		}
		for _, state := range pending {
			if strings.EqualFold(state.Job.Request.Repository, request.Repository) {
				if state.Job.Request != request {
					return domain.ErrImportBusy
				}
				result = state.Job
				return nil
			}
		}
		if len(pending) >= domain.ImportQueueLimit {
			return domain.ErrImportBusy
		}
		now := store.nowUTC()
		result = domain.RepositoryImportJob{ID: uuid.NewString(), Request: request, Stage: "queued", QueuedAt: now, UpdatedAt: now}
		encoded, _ := json.Marshal(importJobState{Version: 1, Job: result})
		_, err = connection.ExecContext(ctx, `INSERT INTO job_runs (`+jobRunColumns+`) VALUES (?,? ,?,NULL,'running',1,0,0,0,?,'',?)`,
			result.ID, domain.ImportJobType, storedTime(now), string(encoded), storedTime(now))
		return err
	})
	return result, err
}

func (store *Store) GetRepositoryImport(ctx context.Context, id string) (domain.RepositoryImportJob, error) {
	var encoded []byte
	if err := store.db.QueryRowContext(ctx, `SELECT details_json FROM job_runs WHERE run_id=? AND job_type=?`, id, domain.ImportJobType).Scan(&encoded); err != nil {
		return domain.RepositoryImportJob{}, mapNotFound(err)
	}
	state, err := decodeImportState(encoded)
	if err != nil || state.Job.ID != id {
		return domain.RepositoryImportJob{}, fmt.Errorf("%w: invalid import record", corestore.ErrInvalid)
	}
	return state.Job, nil
}

// ClaimRepositoryImport returns nil when no task is currently due. The lease
// token is separate from the user-visible DTO and never sent to the browser.
func (store *Store) ClaimRepositoryImport(ctx context.Context, now time.Time, lease time.Duration) (*domain.RepositoryImportJob, string, error) {
	if now.IsZero() || lease < time.Minute || lease > 5*time.Minute {
		return nil, "", domain.ErrImportInvalid
	}
	now = now.UTC()
	var result *domain.RepositoryImportJob
	var token string
	err := store.importTransaction(ctx, func(connection *sql.Conn) error {
		pending, err := pendingImports(ctx, connection)
		if err != nil {
			return err
		}
		// One live lease serializes workers even across overlapping processes.
		for _, state := range pending {
			if state.LeaseUntil != nil && state.LeaseUntil.After(now) {
				return nil
			}
		}
		for _, state := range pending {
			if state.Job.NextAttemptAt != nil && state.Job.NextAttemptAt.After(now) {
				continue
			}
			if state.Job.Attempts >= domain.ImportMaxAttempts {
				state.Job.Stage, state.Job.ErrorCode = "failed", "attempts_exhausted"
				if state.Job.RepositoryID > 0 {
					state.Job.Stage = "partial"
				}
				state.Job.FinishedAt, state.Job.UpdatedAt = &now, now
				state.LeaseToken, state.LeaseUntil = "", nil
				if err := writeImportState(ctx, connection, state, ""); err != nil {
					return err
				}
				continue
			}
			token = uuid.NewString()
			until := now.Add(lease)
			state.LeaseToken, state.LeaseUntil = token, &until
			state.Job.Attempts++
			state.Job.Stage, state.Job.UpdatedAt, state.Job.NextAttemptAt = "resolving", now, nil
			state.Job.ErrorCode = ""
			if state.Job.RepositoryID > 0 {
				state.Job.Stage = "reading"
			}
			if err := writeImportState(ctx, connection, state, ""); err != nil {
				return err
			}
			result = &state.Job
			return nil
		}
		return nil
	})
	return result, token, err
}

// UpdateRepositoryImport applies a checkpoint, retry or terminal result only
// for the unexpired current lease. Completed steps survive retries and restarts.
func (store *Store) UpdateRepositoryImport(ctx context.Context, job domain.RepositoryImportJob, token string) error {
	if token == "" || job.ID == "" {
		return domain.ErrImportLeaseLost
	}
	return store.importTransaction(ctx, func(connection *sql.Conn) error {
		var encoded []byte
		if err := connection.QueryRowContext(ctx, `SELECT details_json FROM job_runs WHERE run_id=? AND job_type=? AND status='running'`, job.ID, domain.ImportJobType).Scan(&encoded); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.ErrImportLeaseLost
			}
			return err
		}
		previous, err := decodeImportState(encoded)
		if err != nil {
			return err
		}
		now := store.nowUTC()
		if previous.LeaseToken != token || previous.LeaseUntil == nil || !previous.LeaseUntil.After(now) {
			return domain.ErrImportLeaseLost
		}
		if job.Request != previous.Job.Request || job.Attempts != previous.Job.Attempts || job.RepositoryID < 0 || previous.Job.RepositoryID > 0 && job.RepositoryID != previous.Job.RepositoryID {
			return domain.ErrImportInvalid
		}
		if (job.Stage == "done" || job.Stage == "partial") && job.RepositoryID <= 0 {
			return domain.ErrImportInvalid
		}
		job.QueuedAt, job.UpdatedAt = previous.Job.QueuedAt, now
		if job.Terminal() {
			job.FinishedAt, job.NextAttemptAt = &now, nil
		} else {
			job.FinishedAt = nil
		}
		state := importJobState{Version: 1, Job: job, LeaseToken: token, LeaseUntil: previous.LeaseUntil}
		if job.Terminal() || job.Stage == "queued" {
			state.LeaseToken, state.LeaseUntil = "", nil
		}
		check, _ := json.Marshal(state)
		if _, err := decodeImportState(check); err != nil {
			return err
		}
		if job.Readme != nil && (job.RepositoryID <= 0 || job.Readme.RepositoryID != job.RepositoryID || utf8.RuneCountInString(job.Readme.Text) > 16000 || utf8.RuneCountInString(job.Readme.Intro) > 1200 || len(job.Readme.Headings) > 8 || job.Readme.FetchedAt.IsZero()) {
			return domain.ErrImportInvalid
		}
		return writeImportState(ctx, connection, state, token)
	})
}

func writeImportState(ctx context.Context, connection *sql.Conn, state importJobState, previousToken string) error {
	encoded, err := json.Marshal(state)
	if err != nil {
		return err
	}
	status := domain.JobRunning
	success, failure := 0, 0
	switch state.Job.Stage {
	case "done":
		status, success = domain.JobSuccess, 1
	case "partial":
		status, success, failure = domain.JobPartial, 1, 1
	case "failed":
		status, failure = domain.JobFailed, 1
	}
	query := `UPDATE job_runs SET details_json=?,status=?,finished_at=?,success_count=?,failure_count=?,error_summary=? WHERE run_id=? AND job_type=? AND status='running'`
	arguments := []any{string(encoded), status, nullableTime(state.Job.FinishedAt), success, failure, state.Job.ErrorCode, state.Job.ID, domain.ImportJobType}
	if previousToken != "" {
		query += ` AND json_extract(details_json,'$.lease_token')=?`
		arguments = append(arguments, previousToken)
	}
	result, err := connection.ExecContext(ctx, query, arguments...)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return domain.ErrImportLeaseLost
	}
	return err
}

// LatestImportReadmes loads a whole bounded feed page in one query. A newer
// import failure cannot erase an earlier successful original-source extract.
func (store *Store) LatestImportReadmes(ctx context.Context, repositoryIDs []int64) (map[int64]*domain.RepositoryReadme, error) {
	result := make(map[int64]*domain.RepositoryReadme)
	if len(repositoryIDs) == 0 {
		return result, nil
	}
	if len(repositoryIDs) > 200 {
		return nil, domain.ErrImportInvalid
	}
	for _, id := range repositoryIDs {
		if id <= 0 {
			return nil, domain.ErrImportInvalid
		}
	}
	query := `SELECT details_json FROM (
SELECT details_json, ROW_NUMBER() OVER (
PARTITION BY json_extract(details_json,'$.job.repository_id')
ORDER BY julianday(json_extract(details_json,'$.job.readme.fetched_at')) DESC,rowid DESC
) AS position FROM job_runs WHERE job_type=?
AND json_extract(details_json,'$.job.repository_id') IN (` + placeholders(len(repositoryIDs)) + `)
AND json_type(details_json,'$.job.readme')='object') WHERE position=1`
	arguments := append([]any{domain.ImportJobType}, anyIDs(repositoryIDs)...)
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var encoded []byte
		if err := rows.Scan(&encoded); err != nil {
			return nil, err
		}
		state, err := decodeImportState(encoded)
		if err != nil {
			return nil, err
		}
		readme := state.Job.Readme
		if readme == nil || readme.RepositoryID != state.Job.RepositoryID {
			return nil, domain.ErrImportInvalid
		}
		if result[readme.RepositoryID] == nil {
			result[readme.RepositoryID] = readme
		}
	}
	return result, rows.Err()
}

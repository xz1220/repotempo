package domain

import (
	"encoding/json"
	"time"
)

type JobStatus string

const (
	JobRunning JobStatus = "running"
	JobSuccess JobStatus = "success"
	JobPartial JobStatus = "partial"
	JobFailed  JobStatus = "failed"
)

func (status JobStatus) Valid() bool {
	switch status {
	case JobRunning, JobSuccess, JobPartial, JobFailed:
		return true
	default:
		return false
	}
}

type JobRun struct {
	RunID        string          `json:"run_id"`
	JobType      string          `json:"job_type"`
	StartedAt    time.Time       `json:"started_at"`
	FinishedAt   *time.Time      `json:"finished_at,omitempty"`
	Status       JobStatus       `json:"status"`
	TargetCount  int             `json:"target_count"`
	SuccessCount int             `json:"success_count"`
	FailureCount int             `json:"failure_count"`
	SkippedCount int             `json:"skipped_count"`
	Details      json.RawMessage `json:"details"`
	ErrorSummary string          `json:"error_summary,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
}

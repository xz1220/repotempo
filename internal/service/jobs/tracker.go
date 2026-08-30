package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/xz1220/github-radar/internal/domain"
)

type Store interface {
	CreateJobRun(context.Context, domain.JobRun) error
	UpdateJobRun(context.Context, domain.JobRun) error
}

type Tracker struct {
	Store Store
	Now   func() time.Time
	NewID func() string
}

type Execution struct {
	tracker Tracker
	Run     domain.JobRun
}

type Outcome struct {
	Status       domain.JobStatus
	TargetCount  int
	SuccessCount int
	FailureCount int
	SkippedCount int
	Details      any
	ErrorSummary string
}

func (tracker Tracker) Start(ctx context.Context, jobType string, details any) (*Execution, error) {
	if tracker.Store == nil {
		return nil, fmt.Errorf("job tracker requires a store")
	}
	if jobType == "" {
		return nil, fmt.Errorf("job type is required")
	}
	if tracker.Now == nil {
		tracker.Now = time.Now
	}
	if tracker.NewID == nil {
		tracker.NewID = func() string { return uuid.NewString() }
	}
	detailsJSON, err := encodeDetails(details)
	if err != nil {
		return nil, err
	}
	now := tracker.Now().UTC()
	run := domain.JobRun{
		RunID:     tracker.NewID(),
		JobType:   jobType,
		StartedAt: now,
		Status:    domain.JobRunning,
		Details:   detailsJSON,
		CreatedAt: now,
	}
	if err := tracker.Store.CreateJobRun(ctx, run); err != nil {
		return nil, fmt.Errorf("create job run: %w", err)
	}
	return &Execution{tracker: tracker, Run: run}, nil
}

func (execution *Execution) Finish(ctx context.Context, outcome Outcome) error {
	if execution == nil {
		return fmt.Errorf("job execution is nil")
	}
	if outcome.Status != domain.JobSuccess && outcome.Status != domain.JobPartial && outcome.Status != domain.JobFailed {
		return fmt.Errorf("finished job status must be success, partial, or failed")
	}
	detailsJSON, err := encodeDetails(outcome.Details)
	if err != nil {
		return err
	}
	finished := execution.tracker.Now().UTC()
	execution.Run.FinishedAt = &finished
	execution.Run.Status = outcome.Status
	execution.Run.TargetCount = outcome.TargetCount
	execution.Run.SuccessCount = outcome.SuccessCount
	execution.Run.FailureCount = outcome.FailureCount
	execution.Run.SkippedCount = outcome.SkippedCount
	execution.Run.Details = detailsJSON
	execution.Run.ErrorSummary = outcome.ErrorSummary
	if err := execution.tracker.Store.UpdateJobRun(ctx, execution.Run); err != nil {
		return fmt.Errorf("update job run: %w", err)
	}
	return nil
}

func encodeDetails(value any) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage(`{}`), nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode job details: %w", err)
	}
	return encoded, nil
}

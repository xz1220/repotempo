package jobs

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestTrackerRejectsMissingStore(t *testing.T) {
	execution, err := (Tracker{}).Start(context.Background(), "snapshot", nil)
	if err == nil || !strings.Contains(err.Error(), "requires a store") {
		t.Fatalf("error = %v, want missing-store error", err)
	}
	if execution != nil {
		t.Fatalf("execution = %#v, want nil", execution)
	}
}

func TestTrackerRejectsInvalidTerminalStatusWithoutUpdatingRun(t *testing.T) {
	invalidStatuses := []domain.JobStatus{
		domain.JobRunning,
		"",
		"canceled",
	}
	for _, status := range invalidStatuses {
		name := string(status)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			store := &fakeJobStore{}
			now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
			tracker := Tracker{
				Store: store,
				Now:   func() time.Time { return now },
				NewID: func() string { return "run-invalid-" + name },
			}
			execution, err := tracker.Start(context.Background(), "snapshot", nil)
			if err != nil {
				t.Fatal(err)
			}

			err = execution.Finish(context.Background(), Outcome{Status: status})
			if err == nil || !strings.Contains(err.Error(), "status must be success, partial, or failed") {
				t.Fatalf("error = %v, want invalid-terminal-status error", err)
			}
			if store.updated.RunID != "" {
				t.Fatalf("invalid terminal status updated store with %#v", store.updated)
			}
			if execution.Run.Status != domain.JobRunning || execution.Run.FinishedAt != nil {
				t.Fatalf("invalid terminal status mutated execution: %#v", execution.Run)
			}
		})
	}
}

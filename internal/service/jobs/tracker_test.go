package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

type fakeJobStore struct {
	created domain.JobRun
	updated domain.JobRun
}

func (store *fakeJobStore) CreateJobRun(_ context.Context, run domain.JobRun) error {
	store.created = run
	return nil
}

func (store *fakeJobStore) UpdateJobRun(_ context.Context, run domain.JobRun) error {
	store.updated = run
	return nil
}

func TestTrackerRecordsLifecycle(t *testing.T) {
	store := &fakeJobStore{}
	times := []time.Time{
		time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC),
		time.Date(2026, 8, 30, 1, 1, 0, 0, time.UTC),
	}
	index := 0
	tracker := Tracker{
		Store: store,
		Now: func() time.Time {
			value := times[index]
			index++
			return value
		},
		NewID: func() string { return "run-1" },
	}
	execution, err := tracker.Start(context.Background(), "snapshot", map[string]any{"scope": "active"})
	if err != nil {
		t.Fatal(err)
	}
	if err := execution.Finish(context.Background(), Outcome{
		Status:       domain.JobPartial,
		TargetCount:  10,
		SuccessCount: 9,
		FailureCount: 1,
		Details:      map[string]any{"failures": []int64{42}},
	}); err != nil {
		t.Fatal(err)
	}
	if store.created.Status != domain.JobRunning || store.updated.Status != domain.JobPartial {
		t.Fatalf("unexpected states: created=%s updated=%s", store.created.Status, store.updated.Status)
	}
	if store.updated.FinishedAt == nil || store.updated.FailureCount != 1 {
		t.Fatalf("unexpected finished run: %#v", store.updated)
	}
}

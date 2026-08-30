package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
)

func TestJobRunsDiscoverySummaryAndBackup(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/project")
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 1,
		FullName:     "owner/project",
		Source:       domain.DiscoverySourceOSSInsight,
		DiscoveredAt: testNow,
	}); err != nil {
		t.Fatalf("merge discovery source: %v", err)
	}
	if err := store.CreateJobRun(ctx, domain.JobRun{
		RunID:   "discover-1",
		JobType: "discover-github-search",
		Status:  domain.JobRunning,
		Details: json.RawMessage(`{"profiles":["popular"],"incomplete_results":false}`),
	}); err != nil {
		t.Fatalf("create job run: %v", err)
	}
	if err := store.UpdateJobRun(ctx, domain.JobRun{
		RunID:        "discover-1",
		Status:       domain.JobPartial,
		TargetCount:  10,
		SuccessCount: 9,
		FailureCount: 1,
		Details:      json.RawMessage(`{"profiles":["popular"],"incomplete_results":true}`),
		ErrorSummary: "one query incomplete",
	}); err != nil {
		t.Fatalf("finish job run: %v", err)
	}
	run, err := store.GetJobRun(ctx, "discover-1")
	if err != nil || run.FinishedAt == nil || run.Status != domain.JobPartial {
		t.Fatalf("finished job run = (%+v, %v)", run, err)
	}
	summary, err := store.DiscoverySummary(ctx)
	if err != nil {
		t.Fatalf("discovery summary: %v", err)
	}
	if len(summary.Runs) != 1 || len(summary.Profiles) != 1 {
		t.Fatalf("discovery summary = %+v", summary)
	}
	if len(summary.Sources) != 4 || summary.Sources[0].RepositoryCount != 1 || summary.Sources[1].RepositoryCount != 1 {
		t.Fatalf("source counts = %+v", summary.Sources)
	}

	backupPath := filepath.Join(t.TempDir(), "export.db")
	if err := store.Backup(ctx, backupPath); err != nil {
		t.Fatalf("create backup: %v", err)
	}
	if err := store.Backup(ctx, backupPath); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("overwrite backup error = %v", err)
	}
	backup, err := Open(ctx, backupPath)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	t.Cleanup(func() { _ = backup.Close() })
	if _, err := backup.GetRepository(ctx, 1); err != nil {
		t.Fatalf("backup missing repository: %v", err)
	}
	if _, err := backup.GetJobRun(ctx, "discover-1"); err != nil {
		t.Fatalf("backup missing job run: %v", err)
	}
}

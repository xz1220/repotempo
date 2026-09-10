package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func enqueueImport(t *testing.T, store *Store, name string) domain.RepositoryImportJob {
	t.Helper()
	job, err := store.EnqueueRepositoryImport(context.Background(), domain.RepositoryImportRequest{Repository: name})
	if err != nil {
		t.Fatal(err)
	}
	return job
}

func claimImport(t *testing.T, store *Store, at time.Time) (*domain.RepositoryImportJob, string) {
	t.Helper()
	job, token, err := store.ClaimRepositoryImport(context.Background(), at, 2*time.Minute)
	if err != nil || job == nil || token == "" {
		t.Fatalf("claim: %+v %q %v", job, token, err)
	}
	return job, token
}

func TestImportQueueDeduplicatesAndBoundsWithoutSchemaChange(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	want := enqueueImport(t, store, "Owner/Repo")
	got, err := store.EnqueueRepositoryImport(ctx, domain.RepositoryImportRequest{Repository: "owner/repo"})
	if err != nil || got.ID != want.ID || got.Request.Focus || got.Stage != "queued" {
		t.Fatalf("duplicate: %+v %v", got, err)
	}
	for _, request := range []domain.RepositoryImportRequest{{Repository: "owner/repo", Focus: true}, {Repository: "owner/repo", Note: "different"}, {Repository: "owner/repo", TopicSlug: "coding-agents"}} {
		if _, err := store.EnqueueRepositoryImport(ctx, request); !errors.Is(err, domain.ErrImportBusy) {
			t.Fatalf("conflicting options silently merged: %v", err)
		}
	}
	for i := 1; i < domain.ImportQueueLimit; i++ {
		enqueueImport(t, store, fmt.Sprintf("owner/repo%d", i))
	}
	if _, err := store.EnqueueRepositoryImport(ctx, domain.RepositoryImportRequest{Repository: "owner/ninth"}); !errors.Is(err, domain.ErrImportBusy) {
		t.Fatalf("queue overflow: %v", err)
	}
	if _, err := store.EnqueueRepositoryImport(ctx, domain.RepositoryImportRequest{Repository: "owner/repo"}); err != nil {
		t.Fatalf("full queue should still allow exact duplicate: %v", err)
	}
	var version, count int
	_ = store.db.QueryRow("PRAGMA user_version").Scan(&version)
	_ = store.db.QueryRow("SELECT count(*) FROM job_runs WHERE job_type=?", domain.ImportJobType).Scan(&count)
	if version != 9 || count != 8 {
		t.Fatalf("schema=%d queue=%d", version, count)
	}
	if _, err := store.GetRepositoryImport(ctx, "unknown"); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestImportQueueRejectsUnsafeInput(t *testing.T) {
	store, _ := newTestStore(t)
	for _, request := range []domain.RepositoryImportRequest{
		{Repository: "https://evil.invalid/owner/repo"}, {Repository: "owner/.."}, {Repository: "owner/repo?token=secret"},
		{Repository: "owner/repo", Note: strings.Repeat("中", 2001)}, {Repository: "owner/repo", Note: string([]byte{0xff})},
	} {
		if _, err := store.EnqueueRepositoryImport(context.Background(), request); !errors.Is(err, domain.ErrImportInvalid) {
			t.Fatalf("bad request accepted: %+v %v", request, err)
		}
	}
}

func TestImportQueueOrdersFractionalTimestampsChronologically(t *testing.T) {
	store, _ := newTestStore(t)
	now := testNow.Add(100 * time.Millisecond)
	store.now = func() time.Time { return now }
	first := enqueueImport(t, store, "owner/first")
	now = now.Add(10 * time.Millisecond)
	enqueueImport(t, store, "owner/second")
	claimed, _ := claimImport(t, store, now)
	if claimed.ID != first.ID {
		t.Fatal("variable-width RFC3339 fractions reversed the queue")
	}
}

func TestImportLeaseRecoveryPreservesCheckpointAndRejectsOldWorker(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	store.now = func() time.Time { return now }
	enqueued := enqueueImport(t, store, "owner/repo")
	enqueueImport(t, store, "owner/other")
	job, token := claimImport(t, store, now)
	if job.ID != enqueued.ID || job.Attempts != 1 || job.Stage != "resolving" {
		t.Fatalf("first claim: %+v", job)
	}
	job.RepositoryID, job.FullName, job.Created, job.Stage = 42, "owner/repo", true, "reading"
	if err := store.UpdateRepositoryImport(ctx, *job, token); err != nil {
		t.Fatal(err)
	}
	if another, _, err := store.ClaimRepositoryImport(ctx, now.Add(time.Minute), 2*time.Minute); err != nil || another != nil {
		t.Fatalf("live worker was duplicated: %+v %v", another, err)
	}
	now = now.Add(2*time.Minute + time.Second)
	if err := store.UpdateRepositoryImport(ctx, *job, token); !errors.Is(err, domain.ErrImportLeaseLost) {
		t.Fatalf("expired owner was accepted: %v", err)
	}
	recovered, newToken := claimImport(t, store, now)
	if recovered.ID != job.ID || recovered.RepositoryID != 42 || recovered.Attempts != 2 || recovered.Stage != "reading" || newToken == token {
		t.Fatalf("checkpoint lost on restart: %+v", recovered)
	}
	if err := store.UpdateRepositoryImport(ctx, *recovered, token); !errors.Is(err, domain.ErrImportLeaseLost) {
		t.Fatalf("stale token accepted: %v", err)
	}
	recovered.Stage = "done"
	if err := store.UpdateRepositoryImport(ctx, *recovered, newToken); err != nil {
		t.Fatal(err)
	}
	completed, _ := store.GetRepositoryImport(ctx, recovered.ID)
	if !completed.Terminal() || completed.FinishedAt == nil || completed.Request.Focus {
		t.Fatalf("completed=%+v", completed)
	}
	if err := store.UpdateRepositoryImport(ctx, *recovered, newToken); !errors.Is(err, domain.ErrImportLeaseLost) {
		t.Fatalf("terminal job was rewritten: %v", err)
	}
	encoded, _ := json.Marshal(completed)
	if strings.Contains(string(encoded), newToken) || strings.Contains(string(encoded), "lease") {
		t.Fatal("lease credential leaked in DTO")
	}
}

func TestImportRetryDelayAndAttemptExhaustion(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	now := testNow
	store.now = func() time.Time { return now }
	enqueued := enqueueImport(t, store, "owner/repo")
	job, token := claimImport(t, store, now)
	next := now.Add(10 * time.Minute)
	job.Stage, job.ErrorCode, job.NextAttemptAt = "queued", "rate_limited", &next
	if err := store.UpdateRepositoryImport(ctx, *job, token); err != nil {
		t.Fatal(err)
	}
	if value, _, err := store.ClaimRepositoryImport(ctx, now.Add(time.Minute), 2*time.Minute); err != nil || value != nil {
		t.Fatalf("retried before Retry-After: %+v %v", value, err)
	}
	now = next
	job, _ = claimImport(t, store, now)
	if job.Attempts != 2 || job.ErrorCode != "" {
		t.Fatalf("retry not reclaimed: %+v", job)
	}
	now = now.Add(3 * time.Minute)
	job, _ = claimImport(t, store, now)
	if job.Attempts != 3 {
		t.Fatal("missing third attempt")
	}
	now = now.Add(3 * time.Minute)
	if job, _, err := store.ClaimRepositoryImport(ctx, now, 2*time.Minute); err != nil || job != nil {
		t.Fatalf("unbounded crash retry: %+v %v", job, err)
	}
	completed, _ := store.GetRepositoryImport(ctx, enqueued.ID)
	if completed.Stage != "failed" || completed.ErrorCode != "attempts_exhausted" {
		t.Fatalf("exhaustion not visible: %+v", completed)
	}
}

func TestImportQueueIsAtomicAcrossProcessesAndReopening(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "queue.db")
	first, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for i := range 12 {
		wg.Add(1)
		go func(store *Store) {
			defer wg.Done()
			job, err := store.EnqueueRepositoryImport(ctx, domain.RepositoryImportRequest{Repository: "owner/one"})
			if err != nil {
				t.Error(err)
				return
			}
			ids <- job.ID
		}([]*Store{first, second}[i%2])
	}
	wg.Wait()
	close(ids)
	unique := map[string]bool{}
	for id := range ids {
		unique[id] = true
	}
	if len(unique) != 1 {
		t.Fatalf("duplicate rows: %v", unique)
	}
	job, _ := claimImport(t, first, testNow)
	if got, _, err := second.ClaimRepositoryImport(ctx, testNow, 2*time.Minute); err != nil || got != nil {
		t.Fatalf("second process duplicated live task: %+v %v", got, err)
	}
	if _, err := second.GetRepositoryImport(ctx, job.ID); err != nil {
		t.Fatal("durable task not visible to second process")
	}
}

func TestLatestImportReadmesKeepsGoodEvidenceAfterFailedRetry(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for index := range 2 {
		enqueueImport(t, store, "owner/repo")
		job, token := claimImport(t, store, testNow)
		job.RepositoryID, job.FullName = 42, "owner/repo"
		if index == 0 {
			job.Stage = "done"
			job.Readme = &domain.RepositoryReadme{RepositoryID: 42, Intro: "这是 README 原文。", Headings: []string{"Project"}, Text: "# Project\n这是 README 原文。", SHA: strings.Repeat("a", 40), Path: "README.md", HTMLURL: "https://github.com/owner/repo/blob/main/README.md", FetchedAt: testNow}
		} else {
			job.Stage, job.ErrorCode = "partial", "readme_unavailable"
		}
		if err := store.UpdateRepositoryImport(ctx, *job, token); err != nil {
			t.Fatal(err)
		}
	}
	values, err := store.LatestImportReadmes(ctx, []int64{42, 43})
	if err != nil || len(values) != 1 || values[42] == nil || values[42].Intro != "这是 README 原文。" {
		t.Fatalf("good evidence erased: %+v %v", values, err)
	}
	if got, err := store.LatestImportReadmes(ctx, nil); err != nil || len(got) != 0 {
		t.Fatal("empty batch should not query all jobs")
	}
}

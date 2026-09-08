package app

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

func importRuntime(t *testing.T, handler http.HandlerFunc) (*Runtime, *atomic.Int64) {
	t.Helper()
	clock := &atomic.Int64{}
	clock.Store(activityTestTime.Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	runtime, err := OpenRuntimeWithOptions(context.Background(), Settings{DatabasePath: t.TempDir() + "/import.db"}, RuntimeOptions{Now: now, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Close() })
	client, err := github.NewClient(github.ClientOptions{BaseURL: server.URL, UserAgent: "import-test", HTTPClient: server.Client(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	runtime.loaded, runtime.github, runtime.discovery = true, client, &config.Discovery{}
	if _, _, err := runtime.store.UpsertTopic(context.Background(), domain.Topic{Slug: "coding-agents", Name: "编程助手", Status: domain.TopicActive}); err != nil {
		t.Fatal(err)
	}
	return runtime, clock
}

func writeImportMetadata(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","description":"An AI coding assistant for developers","stargazers_count":456,"private":false,"fork":false,"topics":["coding-agent"],"created_at":"2026-01-01T00:00:00Z"}`)
}

func writeImportReadme(w http.ResponseWriter) {
	text := "# Project\n\n帮助开发者整理代码的原始介绍。\n\n## Usage\n\n这里是使用说明。"
	digest := sha1.Sum([]byte(fmt.Sprintf("blob %d\x00%s", len(text), text)))
	_ = json.NewEncoder(w).Encode(map[string]any{"type": "file", "encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(text)), "size": len(text), "sha": fmt.Sprintf("%x", digest), "path": "README.md", "html_url": "https://github.com/owner/repo/blob/main/README.md"})
}

func processNextImport(t *testing.T, runtime *Runtime) domain.RepositoryImportJob {
	t.Helper()
	job, token, err := runtime.store.ClaimRepositoryImport(context.Background(), runtime.now(), 2*time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim %+v: %v", job, err)
	}
	runtime.processImport(context.Background(), *job, token)
	value, err := runtime.GetImport(context.Background(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestImportEnqueueHasNoNetworkOrBusinessWritesAndDefaultFocusFalse(t *testing.T) {
	var calls atomic.Int64
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("enqueue requested GitHub") })
	ctx := context.Background()
	job, err := runtime.EnqueueImport(ctx, domain.RepositoryImportRequest{Repository: "https://github.com/Owner/Repo.git"})
	if err != nil || job.Stage != "queued" || job.ID == "" || job.Request.Repository != "owner/repo" || job.Request.Focus || calls.Load() != 0 {
		t.Fatalf("enqueue %+v error=%v calls=%d", job, err, calls.Load())
	}
	if _, err := runtime.store.GetRepository(ctx, 42); err == nil {
		t.Fatal("enqueue prematurely imported a repository")
	}
	for _, request := range []domain.RepositoryImportRequest{{Repository: "https://evil.invalid/owner/repo"}, {Repository: "owner/repo", TopicSlug: "missing"}, {Repository: "owner/repo", Note: strings.Repeat("中", 2001)}} {
		if _, err := runtime.EnqueueImport(ctx, request); !errors.Is(err, domain.ErrImportInvalid) {
			t.Fatalf("invalid request not rejected: %v", err)
		}
	}
	if _, err := runtime.EnqueueImport(ctx, domain.RepositoryImportRequest{Repository: "owner/repo", Focus: true}); !errors.Is(err, domain.ErrImportBusy) {
		t.Fatalf("focus conflict silently lost: %v", err)
	}
}

func TestImportWorkerImportsStarsReadmeAndClassificationWithoutAI(t *testing.T) {
	var calls atomic.Int64
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/repos/owner/repo", "/repositories/42":
			writeImportMetadata(w)
		case "/repos/owner/repo/readme":
			writeImportReadme(w)
		default:
			t.Errorf("unexpected external operation: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})
	requestContext, cancelRequest := context.WithCancel(context.Background())
	job, err := runtime.EnqueueImport(requestContext, domain.RepositoryImportRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	cancelRequest() // Closing the browser must not cancel the persisted task.
	if err := runtime.startImportWorker(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer runtime.stopImportWorker()
	deadline := time.After(3 * time.Second)
	for !job.Terminal() {
		select {
		case <-deadline:
			t.Fatalf("worker did not finish: %+v", job)
		case <-time.After(5 * time.Millisecond):
			job, err = runtime.GetImport(context.Background(), job.ID)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if job.Stage != "done" || job.RepositoryID != 42 || !job.Created || job.Readme == nil || calls.Load() != 3 || job.Attempts != 1 || job.ErrorCode != "" {
		t.Fatalf("completed import %+v calls=%d", job, calls.Load())
	}
	repository, err := runtime.store.GetRepository(context.Background(), 42)
	if err != nil || repository.IsFocus || repository.FirstSeenProfile != "manual-import" {
		t.Fatalf("focus/profile: %+v %v", repository, err)
	}
	detail, err := runtime.store.GetRepositoryDetail(context.Background(), 42, domain.ShanghaiDate(runtime.now()))
	if err != nil || detail.Analysis != nil || len(detail.Metric.Topics) != 1 {
		t.Fatalf("classification or analysis: %+v %v", detail, err)
	}
	if job.Readme.Intro != "帮助开发者整理代码的原始介绍。" {
		t.Fatalf("original README replaced by invented summary: %+v", job.Readme)
	}
	if err := runtime.RunImportWorker(context.Background()); !errors.Is(err, domain.ErrImportBusy) {
		t.Fatal("second worker was not rejected")
	}
}

func TestImportReadmeFailureKeepsRepositoryAndExistingAnalysisFocusSnapshot(t *testing.T) {
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
		} else {
			writeImportMetadata(w)
		}
	})
	ctx := context.Background()
	focus, note := true, "手工备注不可替换"
	_, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 42, FullName: "owner/repo", Source: domain.DiscoverySourceManual, DiscoveredAt: runtime.now().Add(-time.Hour), IsFocus: &focus, ManualNote: &note})
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := runtime.store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 42, SummaryZH: "已有真实人工解读", Source: "manual", AnalyzedAt: runtime.now()})
	if err != nil {
		t.Fatal(err)
	}
	stars := int64(123)
	_, err = runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: 42, SnapshotDate: domain.ShanghaiDate(runtime.now()), CapturedAt: runtime.now(), StarCount: &stars, FetchStatus: domain.FetchSuccess})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.EnqueueImport(ctx, domain.RepositoryImportRequest{Repository: "owner/repo", Note: "replacement"}); err != nil {
		t.Fatal(err)
	}
	job := processNextImport(t, runtime)
	if job.Stage != "partial" || job.ErrorCode != "readme_unavailable" || job.RepositoryID != 42 || job.Readme != nil || job.Created {
		t.Fatalf("missing README should retain import: %+v", job)
	}
	detail, err := runtime.store.GetRepositoryDetail(ctx, 42, domain.ShanghaiDate(runtime.now()))
	if err != nil || detail.Analysis == nil || detail.Analysis.SummaryZH != analysis.SummaryZH || detail.Analysis.Revision != analysis.Revision || !detail.Metric.Repository.IsFocus || detail.Metric.Repository.ManualNote != note {
		t.Fatalf("import replaced user data: %+v %v", detail, err)
	}
	latest, err := runtime.store.GetDailySnapshot(ctx, 42, domain.ShanghaiDate(runtime.now()))
	if err != nil || latest.StarCount == nil || *latest.StarCount != 123 {
		t.Fatalf("import rewrote successful snapshot: %+v %v", latest, err)
	}
}

func TestImportRetriesOnlyIncompleteStepAndSanitizesErrors(t *testing.T) {
	var imports, readmes atomic.Int64
	runtime, clock := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo":
			imports.Add(1)
			writeImportMetadata(w)
		case "/repositories/42":
			writeImportMetadata(w)
		case "/repos/owner/repo/readme":
			if readmes.Add(1) == 1 {
				http.Error(w, `{"message":"secret-token /private/environment failed"}`, 500)
			} else {
				writeImportReadme(w)
			}
		}
	})
	if _, err := runtime.EnqueueImport(context.Background(), domain.RepositoryImportRequest{Repository: "owner/repo", Focus: true}); err != nil {
		t.Fatal(err)
	}
	job := processNextImport(t, runtime)
	if job.Stage != "queued" || job.RepositoryID != 42 || job.NextAttemptAt == nil || !job.NextAttemptAt.After(runtime.now()) || job.ErrorCode != "upstream_unavailable" {
		t.Fatalf("failed read not resumable: %+v", job)
	}
	encoded, _ := json.Marshal(job)
	if strings.Contains(string(encoded), "secret-token") || strings.Contains(string(encoded), "environment") {
		t.Fatal("raw upstream error leaked")
	}
	clock.Add(2 * 60)
	job = processNextImport(t, runtime)
	if job.Stage != "done" || job.Readme == nil || job.Attempts != 2 || imports.Load() != 1 || readmes.Load() != 2 {
		t.Fatalf("retry reran completed metadata: %+v imports=%d readmes=%d", job, imports.Load(), readmes.Load())
	}
	repository, _ := runtime.store.GetRepository(context.Background(), 42)
	if !repository.IsFocus {
		t.Fatal("opt-in focus was lost")
	}
}

func TestImportPrivateRepositoryFailsWithoutBusinessWritesOrAutomaticRetry(t *testing.T) {
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":42,"full_name":"owner/repo","private":true,"stargazers_count":123}`)
	})
	if _, err := runtime.EnqueueImport(context.Background(), domain.RepositoryImportRequest{Repository: "owner/repo"}); err != nil {
		t.Fatal(err)
	}
	job := processNextImport(t, runtime)
	if job.Stage != "failed" || job.ErrorCode != "not_found_or_private" || job.Attempts != 1 || job.NextAttemptAt != nil || job.RepositoryID != 0 {
		t.Fatalf("private import: %+v", job)
	}
	if _, err := runtime.store.GetRepository(context.Background(), 42); err == nil {
		t.Fatal("private repository imported")
	}
}

func TestRuntimeCloseStopsWorkerBeforeClosingDatabaseAndLeavesRecoverableTask(t *testing.T) {
	started := make(chan struct{})
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/readme") {
			close(started)
			<-r.Context().Done()
			return
		}
		writeImportMetadata(w)
	})
	job, err := runtime.EnqueueImport(context.Background(), domain.RepositoryImportRequest{Repository: "owner/repo"})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.startImportWorker(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not enter README request")
	}
	closed := make(chan error, 1)
	go func() { closed <- runtime.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not cancel the running request")
	}
	reopened, err := sqlite.Open(context.Background(), runtime.settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := reopened.GetRepositoryImport(context.Background(), job.ID)
	if err != nil || recovered.RepositoryID != 42 || recovered.Stage != "queued" || recovered.ErrorCode != "interrupted" {
		t.Fatalf("shutdown lost recovery checkpoint: %+v %v", recovered, err)
	}
}

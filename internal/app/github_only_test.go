package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestGitHubOnlyDailyFindsNewProjectAndSnapshotsIt(t *testing.T) {
	var searchCalls, snapshotCalls, unexpectedCalls atomic.Int64
	repoJSON := `{"id":71,"full_name":"team/codex","html_url":"https://github.com/team/codex","description":"AI coding assistant","stargazers_count":90,"forks_count":1,"topics":["coding-agent"],"created_at":"2026-09-01T00:00:00Z"}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/search/repositories":
			searchCalls.Add(1)
			fmt.Fprintf(w, `{"total_count":1,"incomplete_results":false,"items":[%s]}`, repoJSON)
		case "/repositories/71":
			snapshotCalls.Add(1)
			fmt.Fprint(w, repoJSON)
		default:
			unexpectedCalls.Add(1)
			http.Error(w, "unexpected request", http.StatusForbidden)
		}
	}))
	defer server.Close()
	directory := t.TempDir()
	configuration := filepath.Join(directory, "discovery.yaml")
	writeFixture(t, configuration, fmt.Sprintf(`version: 1
github:
  api_base_url: %s/
  user_agent: github-radar-test
  timeout: 2s
  core_interval: 1ms
  search_interval: 1ms
  secondary_backoff: 1m
  server_backoff: 1ms
  max_retries: 0
profiles:
  - name: recent-agents
    schedule: daily
    sort: stars
    order: desc
    partition: {by: none, max_depth: 0}
    queries:
      - topic: coding-agent
`, server.URL))
	runtime, err := OpenRuntimeWithOptions(context.Background(), Settings{
		DatabasePath: filepath.Join(directory, "radar.db"), DiscoveryConfig: configuration,
		TopicsConfig:    filepath.Join("..", "..", "config", "topics.example.yaml"),
		ExportDirectory: filepath.Join(directory, "exports"), BackupDirectory: filepath.Join(directory, "backups"),
		ExportRetention: 30, BackupRetention: 30,
	}, RuntimeOptions{HTTPClient: server.Client(), Now: func() time.Time { return time.Date(2026, 9, 7, 2, 0, 0, 0, time.UTC) }, CheckRuntime: func(Settings) DoctorReport { return DoctorReport{Healthy: true} }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	report, err := runtime.RunDaily(context.Background(), false)
	if err != nil || report.Partial() || report.Fatal {
		t.Fatalf("daily = %+v %v", report, err)
	}
	if report.Discovery.CreatedCount != 1 || report.Snapshot.SuccessCount != 1 || searchCalls.Load() != 1 || snapshotCalls.Load() != 1 || unexpectedCalls.Load() != 0 {
		t.Fatalf("unexpected daily: %+v", report)
	}
	snapshot, err := runtime.store.GetDailySnapshot(context.Background(), 71, domain.Date("2026-09-07"))
	if err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 90 {
		t.Fatalf("snapshot = %+v %v", snapshot, err)
	}
	topics, err := runtime.store.ListRepositoryTopics(context.Background(), 71)
	if err != nil || len(topics) != 1 || topics[0].Slug != "coding-agents" {
		t.Fatalf("categories = %+v %v", topics, err)
	}
	// Running again on the same date neither duplicates snapshots nor searches
	// a completed daily profile again.
	repeated, err := runtime.RunDaily(context.Background(), false)
	if err != nil || repeated.Snapshot.SkippedCount != 1 || searchCalls.Load() != 1 || snapshotCalls.Load() != 1 {
		t.Fatalf("rerun = %+v %v", repeated, err)
	}
}

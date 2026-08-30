package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestRunDailyContinuesSnapshotAfterOSSDiscoveryFailure(t *testing.T) {
	var repositoryRequests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/oss":
			http.Error(writer, "temporarily unavailable", http.StatusServiceUnavailable)
		case "/repositories/1":
			repositoryRequests.Add(1)
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{
                    "id":1,"node_id":"R_1","full_name":"owner/repo",
                    "html_url":"https://github.com/owner/repo","description":"fixture",
                    "language":"Go","stargazers_count":42,"forks_count":2,
                    "fork":false,"archived":false,"private":false,"topics":["agents"],
                    "created_at":"2025-01-01T00:00:00Z","updated_at":"2026-08-30T00:00:00Z",
                    "pushed_at":"2026-08-30T00:00:00Z"
                }`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	discoveryPath := filepath.Join(directory, "discovery.yaml")
	topicsPath := filepath.Join(directory, "topics.yaml")
	writeFixture(t, discoveryPath, fmt.Sprintf(`version: 1
github:
  api_base_url: %s/
  api_version: "2026-03-10"
  user_agent: github-radar-test
  timeout: 2s
  core_interval: 1ms
  search_interval: 1ms
  secondary_backoff: 1m
  server_backoff: 1ms
  max_retries: 0
ossinsight:
  enabled: true
  base_url: %s/oss
  language: All
  timeout: 2s
  windows:
    - name: today
      period: past_24_hours
profiles: []
manual:
  files: []
legacy:
  database_path: ""
`, server.URL, server.URL))
	writeFixture(t, topicsPath, `version: 1
topics:
  - slug: agents
    name: Agents
    status: active
`)
	now := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	runtime, err := OpenRuntimeWithOptions(context.Background(), Settings{
		DatabasePath:    filepath.Join(directory, "radar.db"),
		ExportDirectory: filepath.Join(directory, "exports"),
		BackupDirectory: filepath.Join(directory, "backups"),
		DiscoveryConfig: discoveryPath,
		TopicsConfig:    topicsPath,
		ExportRetention: 30,
		BackupRetention: 30,
	}, RuntimeOptions{
		Now:        func() time.Time { return now },
		HTTPClient: server.Client(),
		CheckRuntime: func(Settings) DoctorReport {
			usage := 20.0
			return DoctorReport{Healthy: true, DiskUsage: &usage}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	_, _, err = runtime.store.UpsertRepository(context.Background(), domain.RepositoryObservation{
		GitHubRepoID:     1,
		FullName:         "owner/repo",
		HTMLURL:          "https://github.com/owner/repo",
		Source:           domain.DiscoverySourceLegacy,
		DiscoveredAt:     now.AddDate(0, 0, -1),
		MonitoringStatus: domain.MonitoringActive,
		GitHubStatus:     domain.GitHubActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	report, err := runtime.RunDaily(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if repositoryRequests.Load() != 1 {
		t.Fatalf("repository requests = %d, want 1", repositoryRequests.Load())
	}
	if !report.Discovery.Partial() || report.Snapshot.SuccessCount != 1 || report.Snapshot.FailureCount != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Exports) != 3 {
		t.Fatalf("exports = %#v", report.Exports)
	}
	for _, exported := range report.Exports {
		if _, err := os.Stat(exported.Path); err != nil {
			t.Fatalf("missing %s export %s: %v", exported.Format, exported.Path, err)
		}
	}

	runs, err := runtime.store.ListJobRuns(context.Background(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var foundDaily, foundDiscovery bool
	for _, run := range runs {
		switch run.JobType {
		case "run-daily":
			foundDaily = true
			if run.Status != domain.JobPartial {
				t.Fatalf("daily status = %s", run.Status)
			}
			var details DailyJobDetails
			if err := json.Unmarshal(run.Details, &details); err != nil {
				t.Fatal(err)
			}
			if details.Discovery.OSS == nil || details.Discovery.OSS.Error == "" {
				t.Fatalf("daily details omitted OSS failure: %#v", details.Discovery)
			}
		case "discover":
			foundDiscovery = true
			if run.Status != domain.JobPartial {
				t.Fatalf("discovery status = %s", run.Status)
			}
		}
	}
	if !foundDaily || !foundDiscovery {
		t.Fatalf("job runs did not include both parent and discovery: %#v", runs)
	}
}

func TestCleanupRetentionOnlyRemovesExpiredRadarArtifacts(t *testing.T) {
	directory := t.TempDir()
	oldArtifact := filepath.Join(directory, "github-radar-old.json")
	unrelated := filepath.Join(directory, "keep.txt")
	writeFixture(t, oldArtifact, "old")
	writeFixture(t, unrelated, "keep")
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(oldArtifact, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unrelated, old, old); err != nil {
		t.Fatal(err)
	}
	cleaned, err := cleanupRetention(directory, 30, time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 1 || cleaned[0] != oldArtifact {
		t.Fatalf("cleaned = %#v", cleaned)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Fatalf("unrelated file was removed: %v", err)
	}
}

func TestCleanupRetentionRejectsBroadDirectories(t *testing.T) {
	if _, err := cleanupRetention(".", 30, time.Now()); err == nil {
		t.Fatal("expected current directory guard")
	}
	if _, err := cleanupRetention(string(filepath.Separator), 30, time.Now()); err == nil {
		t.Fatal("expected filesystem root guard")
	}
	if home, err := os.UserHomeDir(); err == nil {
		if _, err := cleanupRetention(home, 30, time.Now()); err == nil {
			t.Fatal("expected home directory guard")
		}
	}
}

func writeFixture(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

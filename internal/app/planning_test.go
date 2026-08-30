package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestReadOnlyPlanningUsesExistingStateWithoutChangingDatabase(t *testing.T) {
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "radar.db")
	discoveryPath := filepath.Join(directory, "discovery.yaml")
	topicsPath := filepath.Join(directory, "topics.yaml")
	writeFixture(t, discoveryPath, `version: 1
github:
  api_base_url: https://api.github.invalid/
  api_version: "2026-03-10"
  user_agent: github-radar-test
  timeout: 2s
  core_interval: 1ms
  search_interval: 1ms
  secondary_backoff: 1m
  server_backoff: 1ms
  max_retries: 0
ossinsight:
  enabled: false
profiles:
  - name: daily-profile
    schedule: daily
    sort: stars
    order: desc
    partition:
      by: none
      max_depth: 0
    queries:
      - text: radar in:name
        fork: false
        archived: false
manual:
  files: []
legacy:
  database_path: ""
`)
	writeFixture(t, topicsPath, `version: 1
topics:
  - slug: agents
    name: Agents
    status: active
`)
	now := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	settings := Settings{DatabasePath: databasePath, DiscoveryConfig: discoveryPath, TopicsConfig: topicsPath}
	writable, err := OpenRuntimeWithOptions(context.Background(), settings, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := writable.ensureTopics(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, _, err = writable.store.UpsertRepository(context.Background(), domain.RepositoryObservation{
		GitHubRepoID:     7,
		FullName:         "owner/repo",
		Source:           domain.DiscoverySourceLegacy,
		DiscoveredAt:     now.AddDate(0, 0, -1),
		MonitoringStatus: domain.MonitoringActive,
		GitHubStatus:     domain.GitHubActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	finished := now.Add(-time.Minute)
	details, _ := json.Marshal(DiscoverReport{Profiles: []SearchProfileReport{{Name: "daily-profile", Due: true, QueryReports: []SearchQueryReport{}}}})
	if err := writable.store.CreateJobRun(context.Background(), domain.JobRun{
		RunID:      "discover-fixture",
		JobType:    "discover",
		StartedAt:  now.Add(-2 * time.Minute),
		FinishedAt: &finished,
		Status:     domain.JobSuccess,
		Details:    details,
		CreatedAt:  now.Add(-2 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	beforeHash := fileHash(t, databasePath)
	beforeEntries := directoryEntries(t, directory)

	healthy := func(Settings) DoctorReport { return DoctorReport{Healthy: true} }
	planner, err := OpenRuntimeWithOptions(context.Background(), settings, RuntimeOptions{
		Now:          func() time.Time { return now },
		CheckRuntime: healthy,
		ReadOnly:     true,
	})
	if err != nil {
		t.Fatal(err)
	}
	snapshotPlan, err := planner.Snapshot(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if snapshotPlan.TargetCount != 1 {
		t.Fatalf("snapshot target = %d, want 1", snapshotPlan.TargetCount)
	}
	assignment, err := planner.AssignTopic(context.Background(), "owner/repo", "agents", true)
	if err != nil {
		t.Fatal(err)
	}
	if assignment.Assignment.RepositoryID != 7 || assignment.Assignment.TopicID == 0 {
		t.Fatalf("assignment plan = %#v", assignment)
	}
	dailyPlan, err := planner.RunDaily(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if dailyPlan.Snapshot.TargetCount != 1 || len(dailyPlan.Discovery.Profiles) != 1 || !dailyPlan.Discovery.Profiles[0].Skipped {
		t.Fatalf("daily plan = %#v", dailyPlan)
	}
	if err := planner.Close(); err != nil {
		t.Fatal(err)
	}
	if afterHash := fileHash(t, databasePath); afterHash != beforeHash {
		t.Fatal("read-only planning changed the target database")
	}
	if afterEntries := directoryEntries(t, directory); !reflect.DeepEqual(afterEntries, beforeEntries) {
		t.Fatalf("planning changed directory entries: before=%v after=%v", beforeEntries, afterEntries)
	}
}

func TestIncompleteDiscoveryProfileNeverAdvancesScheduleGate(t *testing.T) {
	now := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	runtime, err := OpenRuntimeWithOptions(context.Background(), Settings{DatabasePath: ":memory:"}, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	finished := now.Add(time.Minute)
	details, _ := json.Marshal(DiscoverReport{Profiles: []SearchProfileReport{{
		Name: "unstable", Due: true, IncompleteResults: true,
		Error: "github search returned incomplete_results=true",
	}}})
	if err := runtime.store.CreateJobRun(context.Background(), domain.JobRun{
		RunID: "incomplete", JobType: "discover", StartedAt: now, FinishedAt: &finished,
		Status: domain.JobPartial, Details: details, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	last, err := runtime.lastProfileSuccess(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := last["unstable"]; ok {
		t.Fatalf("incomplete profile advanced schedule: %#v", last)
	}
}

func fileHash(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(contents)
}

func directoryEntries(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry.Name())
	}
	sort.Strings(result)
	return result
}

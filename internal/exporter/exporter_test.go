package exporter

import (
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

func sampleDataset() Dataset {
	now := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	stars := int64(123)
	return Dataset{
		GeneratedAt:      now,
		Repositories:     []domain.Repository{{GitHubRepoID: 42, FullName: "owner/repo", FirstSeenAt: now, LastDiscoveredAt: now, CreatedAt: now, UpdatedAt: now, DiscoverySources: []domain.DiscoverySource{domain.DiscoverySourceManual}, PreviousNames: []string{}}},
		DailySnapshots:   []domain.DailySnapshot{{RepositoryID: 42, SnapshotDate: "2026-08-30", CapturedAt: now, StarCount: &stars, FetchStatus: domain.FetchSuccess, CreatedAt: now}},
		Topics:           []domain.Topic{{ID: 1, Slug: "ai-agent", Name: "AI Agent", Status: domain.TopicActive, CreatedAt: now, UpdatedAt: now}},
		RepositoryTopics: []domain.RepositoryTopic{{RepositoryID: 42, TopicID: 1, Source: domain.TopicSourceManual, Confirmed: true, AssignedAt: now, UpdatedAt: now}},
		JobRuns:          []domain.JobRun{{RunID: "run-1", JobType: "snapshot", StartedAt: now, Status: domain.JobSuccess, Details: json.RawMessage(`{}`), CreatedAt: now}},
	}
}

func TestWriteJSONContainsAllFiveTables(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.json")
	if err := writeJSON(path, sampleDataset()); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Dataset
	if err := json.Unmarshal(content, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded.Repositories) != 1 || len(decoded.DailySnapshots) != 1 || len(decoded.Topics) != 1 || len(decoded.RepositoryTopics) != 1 || len(decoded.JobRuns) != 1 {
		t.Fatalf("incomplete export: %#v", decoded)
	}
}

func TestWriteCSVDirectoryPreservesNullFailureStar(t *testing.T) {
	dataset := sampleDataset()
	dataset.Repositories[0].Description = "=HYPERLINK(\"https://invalid.example\")"
	dataset.DailySnapshots[0].FetchStatus = domain.FetchFailed
	dataset.DailySnapshots[0].StarCount = nil
	path := filepath.Join(t.TempDir(), "csv-export")
	if err := writeCSVDirectory(path, dataset); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(path, "daily_snapshots.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	rows, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := rows[1][3]; got != "" {
		t.Fatalf("failed star count = %q, expected empty", got)
	}
	for _, name := range []string{"repositories.csv", "daily_snapshots.csv", "topics.csv", "repository_topics.csv", "job_runs.csv"} {
		if _, err := os.Stat(filepath.Join(path, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
	repositoryFile, err := os.Open(filepath.Join(path, "repositories.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer repositoryFile.Close()
	repositoryRows, err := csv.NewReader(repositoryFile).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := repositoryRows[1][4]; len(got) == 0 || got[0] != '\'' {
		t.Fatalf("formula-like CSV description was not neutralized: %q", got)
	}
}

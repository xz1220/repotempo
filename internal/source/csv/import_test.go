package csv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestImportCSVPreservesOnlyRealObservations(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "testdata", "csv", "history.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := Import(file, Options{Location: time.FixedZone("CST", 8*60*60)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(result.Candidates))
	}
	if len(result.Observations) != 1 {
		t.Fatalf("observations = %+v, want only the timestamped row", result.Observations)
	}
	observation := result.Observations[0]
	if observation.RepositoryID != 1 || observation.StarCount != 10 || observation.FixedPanel {
		t.Fatalf("observation = %+v", observation)
	}
	if result.Candidates[0].Repository.AbsoluteStars != nil {
		t.Fatal("CSV observation must not also become repository current stars")
	}
	if len(result.Candidates[0].PreviousNames) != 1 || result.Candidates[0].PreviousNames[0] != "old/name" {
		t.Fatalf("previous names = %v", result.Candidates[0].PreviousNames)
	}
	foundMissingTime := false
	for _, warning := range result.Warnings {
		if warning.Code == "missing_observed_at" {
			foundMissingTime = true
		}
	}
	if !foundMissingTime {
		t.Fatalf("warnings = %+v, want missing_observed_at", result.Warnings)
	}
}

func TestImportCSVDerivesDateFromRealTimestamp(t *testing.T) {
	raw := "github_repo_id,full_name,observed_at,github_stars\n7,owner/repo,2026-08-29T18:30:00Z,0\n"
	location := time.FixedZone("CST", 8*60*60)
	result, err := Import(strings.NewReader(raw), Options{Location: location})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Observations) != 1 || result.Observations[0].SnapshotDate.Format("2006-01-02") != "2026-08-30" || result.Observations[0].StarCount != 0 {
		t.Fatalf("observations = %+v", result.Observations)
	}
}

func TestImportCSVRequiresPermanentID(t *testing.T) {
	_, err := Import(strings.NewReader("full_name,stars\nowner/repo,10\n"), Options{})
	if err == nil || !strings.Contains(err.Error(), "github_repo_id") {
		t.Fatalf("error = %v, want missing permanent ID", err)
	}
}

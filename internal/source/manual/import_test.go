package manual

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/source"
)

type fakeResolver struct{}

func (fakeResolver) ResolveRepository(_ context.Context, fullName string, expectedID int64) (source.Repository, error) {
	switch fullName {
	case "owner/repo":
		stars := int64(10)
		return source.Repository{ID: 42, FullName: "new-owner/repo", AbsoluteStars: &stars}, nil
	case "owner/fork":
		return source.Repository{ID: 43, FullName: fullName, Fork: true}, nil
	default:
		return source.Repository{}, errors.New("not found")
	}
}

func TestImportManualAlwaysResolvesGitHubIDAndAllowsFork(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "..", "testdata", "manual", "watchlist.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	now := time.Date(2026, 8, 30, 8, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	result, err := Import(context.Background(), file, fakeResolver{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 2 {
		t.Fatalf("candidates = %+v", result.Candidates)
	}
	first := result.Candidates[0]
	if first.Repository.ID != 42 || first.Repository.FullName != "new-owner/repo" || !first.IsFocus || first.Source != "manual" || first.Profile != "config-watchlist" {
		t.Fatalf("first candidate = %+v", first)
	}
	if len(first.PreviousNames) != 1 || first.PreviousNames[0] != "owner/repo" {
		t.Fatalf("previous names = %v", first.PreviousNames)
	}
	if !result.Candidates[1].Repository.Fork || result.Candidates[1].MonitorStatus != "paused" {
		t.Fatalf("manual fork = %+v", result.Candidates[1])
	}
}

func TestImportManualRejectsUnknownFields(t *testing.T) {
	raw := "version: 1\nrepositories:\n  - full_name: owner/repo\n    mystery: true\n"
	_, err := Import(context.Background(), strings.NewReader(raw), fakeResolver{}, time.Now())
	if err == nil || !strings.Contains(err.Error(), "field mystery not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestImportManualReportsResolutionFailure(t *testing.T) {
	raw := "version: 1\nrepositories:\n  - full_name: missing/repo\n    monitoring_status: active\n"
	result, err := Import(context.Background(), strings.NewReader(raw), fakeResolver{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 0 || len(result.Warnings) != 1 || result.Warnings[0].Code != "github_resolution_failed" {
		t.Fatalf("result = %+v", result)
	}
}

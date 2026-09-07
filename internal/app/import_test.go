package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
)

func TestImportCSVRejectsUnverifiedRepositoryIDAndObservation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/owner/repo" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
            "id":2,"node_id":"R_2","full_name":"owner/repo",
            "html_url":"https://github.com/owner/repo","description":"fixture",
            "language":"Go","stargazers_count":11,"forks_count":1,
            "fork":false,"archived":false,"private":false,"topics":[],
            "created_at":"2025-01-01T00:00:00Z","updated_at":"2026-08-30T00:00:00Z",
            "pushed_at":"2026-08-30T00:00:00Z"
        }`))
	}))
	defer server.Close()
	directory := t.TempDir()
	discoveryPath := filepath.Join(directory, "discovery.yaml")
	csvPath := filepath.Join(directory, "history.csv")
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
  enabled: false
profiles: []
manual:
  files: []
legacy:
  database_path: ""
`, server.URL))
	writeFixture(t, csvPath, `github_repo_id,full_name,star_count,snapshot_date,observed_at
1,owner/repo,10,2026-08-29,2026-08-29T01:00:00Z
`)
	runtime, err := OpenRuntimeWithOptions(context.Background(), Settings{
		DatabasePath:    ":memory:",
		DiscoveryConfig: discoveryPath,
	}, RuntimeOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	report, err := runtime.ImportLegacy(context.Background(), ImportOptions{CSVPaths: []string{csvPath}, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Partial() || report.CandidateCount != 0 || report.ObservationCount != 0 {
		t.Fatalf("unexpected report: %#v", report)
	}
	stages := make(map[string]bool)
	for _, failure := range report.Failures {
		stages[failure.Stage] = true
	}
	if !stages["csv-identity"] || !stages["csv-observation"] {
		t.Fatalf("identity failures were not explicit: %#v", report.Failures)
	}
}

func TestConfiguredWatchlistTopicCannotOverrideManualVeto(t *testing.T) {
	topicSource, confirmed := candidateTopicSource(source.Candidate{Source: "manual", Profile: "config-watchlist"})
	if topicSource != domain.TopicSourceAuto || confirmed {
		t.Fatalf("configured topic source = (%s, %t), want unconfirmed auto", topicSource, confirmed)
	}
}

func TestApplyImportedEvidencePreservesRankAndWindowWithoutChangingStars(t *testing.T) {
	stars := int64(1000)
	snapshotValue := domain.DailySnapshot{StarCount: &stars}
	if err := applyImportedEvidence(&snapshotValue, map[string]string{
		"legacy_rank":         "7",
		"legacy_window_stars": "88",
		"oss_total_score":     "12.5",
	}); err != nil {
		t.Fatal(err)
	}
	if snapshotValue.OSSTodayRank == nil || *snapshotValue.OSSTodayRank != 7 {
		t.Fatalf("rank = %#v", snapshotValue.OSSTodayRank)
	}
	if snapshotValue.OSSWindowStars == nil || *snapshotValue.OSSWindowStars != 88 {
		t.Fatalf("window stars = %#v", snapshotValue.OSSWindowStars)
	}
	if snapshotValue.StarCount == nil || *snapshotValue.StarCount != 1000 {
		t.Fatalf("absolute stars changed: %#v", snapshotValue.StarCount)
	}
}

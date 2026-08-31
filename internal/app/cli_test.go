package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/service/discovery"
	"github.com/xz1220/github-radar/internal/service/snapshot"
	"github.com/xz1220/github-radar/internal/service/watch"
)

type fakeCommandApplication struct {
	discoverOptions DiscoverOptions
	discoverReport  DiscoverReport
	snapshotDryRun  bool
	dailyReport     DailyReport
	closed          bool
	analysisRepo    string
	analysisInput   domain.RepositoryAnalysis
}

func (application *fakeCommandApplication) Discover(_ context.Context, options DiscoverOptions) (DiscoverReport, error) {
	application.discoverOptions = options
	return application.discoverReport, nil
}

func (application *fakeCommandApplication) Snapshot(_ context.Context, dryRun bool) (SnapshotCommandReport, error) {
	application.snapshotDryRun = dryRun
	return SnapshotCommandReport{Report: snapshot.Report{Date: "2026-08-30", TargetCount: 4}, DryRun: dryRun}, nil
}

func (application *fakeCommandApplication) ImportLegacy(context.Context, ImportOptions) (ImportReport, error) {
	return ImportReport{}, nil
}

func (application *fakeCommandApplication) ImportAnalysis(_ context.Context, repository string, analysis domain.RepositoryAnalysis) (domain.RepositoryAnalysis, error) {
	application.analysisRepo = repository
	application.analysisInput = analysis
	analysis.RepositoryID = 42
	analysis.Revision = 2
	return analysis, nil
}

func (application *fakeCommandApplication) ListTopics(context.Context) ([]domain.Topic, error) {
	return []domain.Topic{{ID: 1, Slug: "agents", Name: "Agents"}}, nil
}

func (application *fakeCommandApplication) AssignTopic(context.Context, string, string, bool) (domain.TopicAssignmentResult, error) {
	return domain.TopicAssignmentResult{Changed: true}, nil
}

func (application *fakeCommandApplication) RemoveTopic(context.Context, string, string, bool) (bool, error) {
	return true, nil
}

func (application *fakeCommandApplication) WatchAdd(context.Context, string, string, bool, bool) (watch.AddResult, error) {
	return watch.AddResult{}, nil
}

func (application *fakeCommandApplication) WatchSet(context.Context, string, domain.MonitoringStatus, bool) (domain.Repository, error) {
	return domain.Repository{}, nil
}

func (application *fakeCommandApplication) Export(context.Context, ExportOptions) (ExportReport, error) {
	return ExportReport{}, nil
}

func (application *fakeCommandApplication) RunDaily(context.Context, bool) (DailyReport, error) {
	return application.dailyReport, nil
}

func (application *fakeCommandApplication) Serve(context.Context, string) error { return nil }

func (application *fakeCommandApplication) Close() error {
	application.closed = true
	return nil
}

func testCLI(application CommandApplication) (*CLI, *bytes.Buffer, *bytes.Buffer) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cli := &CLI{
		Stdout: stdout,
		Stderr: stderr,
		LoadSettings: func() (Settings, error) {
			return Settings{DatabasePath: ":memory:", GitHubToken: "fixture-key"}, nil
		},
		OpenRuntime:  func(context.Context, Settings) (CommandApplication, error) { return application, nil },
		CheckRuntime: func(Settings) DoctorReport { return DoctorReport{Healthy: true} },
	}
	return cli, stdout, stderr
}

func TestCLIDiscoverJSONReportsPartialAndForwardsOptions(t *testing.T) {
	application := &fakeCommandApplication{discoverReport: DiscoverReport{
		Source:         "github-search",
		CandidateCount: 12,
		FailureCount:   1,
		Failures:       []OperationFailure{{Stage: "github-search", Message: "incomplete results"}},
		Registry:       &discovery.RegistryReport{},
	}}
	cli, stdout, _ := testCLI(application)
	code := cli.Run(context.Background(), []string{"discover", "--source", "github-search", "--profile", "agents", "--dry-run", "--json"})
	if code != ExitPartial {
		t.Fatalf("exit code = %d, want %d", code, ExitPartial)
	}
	if !application.discoverOptions.DryRun || application.discoverOptions.Profile != "agents" {
		t.Fatalf("options = %#v", application.discoverOptions)
	}
	var envelope outputEnvelope
	if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Status != "partial" || envelope.Command != "discover" {
		t.Fatalf("envelope = %#v", envelope)
	}
	if !application.closed {
		t.Fatal("runtime was not closed")
	}
}

func TestCLISnapshotDryRunHasHumanOutput(t *testing.T) {
	application := &fakeCommandApplication{}
	cli, stdout, _ := testCLI(application)
	code := cli.Run(context.Background(), []string{"snapshot", "--dry-run"})
	if code != ExitSuccess || !application.snapshotDryRun {
		t.Fatalf("code=%d dry-run=%t", code, application.snapshotDryRun)
	}
	if !strings.Contains(stdout.String(), "4 active repositories") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCLIImportsStoredRepositoryAnalysisFromJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "analysis.json")
	if err := os.WriteFile(path, []byte(`{
  "summary_zh": "这是项目摘要。",
  "key_points": ["能力一"],
  "use_cases": ["场景一"],
  "technical_notes": "Go 项目",
  "model": "gpt-test"
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	application := &fakeCommandApplication{}
	cli, stdout, _ := testCLI(application)
	code := cli.Run(context.Background(), []string{"analysis", "import", "--repo", "owner/project", "--file", path})
	if code != ExitSuccess {
		t.Fatalf("exit code = %d, want %d", code, ExitSuccess)
	}
	if application.analysisRepo != "owner/project" || application.analysisInput.SummaryZH != "这是项目摘要。" ||
		application.analysisInput.Source != "codex" || application.analysisInput.Model != "gpt-test" ||
		len(application.analysisInput.KeyPoints) != 1 || len(application.analysisInput.UseCases) != 1 {
		t.Fatalf("analysis import = repo %q input %+v", application.analysisRepo, application.analysisInput)
	}
	if !strings.Contains(stdout.String(), "Stored analysis revision 2 for owner/project") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCLIServeUsesChineseStartupMessage(t *testing.T) {
	application := &fakeCommandApplication{}
	cli, stdout, _ := testCLI(application)
	cli.LoadSettings = func() (Settings, error) {
		return Settings{DatabasePath: ":memory:", ListenAddress: "127.0.0.1:8787", Locale: "zh-CN"}, nil
	}

	if code := cli.Run(context.Background(), []string{"serve"}); code != ExitSuccess {
		t.Fatalf("exit code = %d", code)
	}
	if got := stdout.String(); got != "GitHub Radar 正在监听 http://127.0.0.1:8787\n" {
		t.Fatalf("stdout = %q", got)
	}
}

func TestCLIUsesStableUsageExitCode(t *testing.T) {
	cli, _, stderr := testCLI(&fakeCommandApplication{})
	code := cli.Run(context.Background(), []string{"topic", "assign", "--repo", "owner/repo"})
	if code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
	if !strings.Contains(stderr.String(), "--repo and --topic are required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIRedactsEnvironmentTokenFromErrors(t *testing.T) {
	cli, _, stderr := testCLI(nil)
	cli.OpenRuntime = func(context.Context, Settings) (CommandApplication, error) {
		return nil, errors.New("request failed with fixture-key")
	}
	code := cli.Run(context.Background(), []string{"topic", "list"})
	if code != ExitFailure {
		t.Fatalf("exit code = %d", code)
	}
	if strings.Contains(stderr.String(), "fixture-key") || !strings.Contains(stderr.String(), "[redacted]") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCLIHealthyAndUnhealthyDoctorExitCodes(t *testing.T) {
	cli, _, _ := testCLI(&fakeCommandApplication{})
	if code := cli.Run(context.Background(), []string{"doctor"}); code != ExitSuccess {
		t.Fatalf("healthy exit code = %d", code)
	}
	cli.CheckRuntime = func(Settings) DoctorReport {
		return DoctorReport{Healthy: false, Checks: []DoctorCheck{{Name: "disk", Status: CheckFail, Message: "full"}}}
	}
	if code := cli.Run(context.Background(), []string{"doctor"}); code != ExitFailure {
		t.Fatalf("unhealthy exit code = %d", code)
	}
}

func TestCLIRunDailyFatalUsesFailureExit(t *testing.T) {
	application := &fakeCommandApplication{dailyReport: DailyReport{Fatal: true, Failures: []OperationFailure{{Stage: "snapshot", Message: "failed"}}}}
	cli, _, _ := testCLI(application)
	if code := cli.Run(context.Background(), []string{"run-daily"}); code != ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, ExitFailure)
	}
}

func TestCLIRunDailyUnhealthyPreflightDoesNotOpenTarget(t *testing.T) {
	applicationOpened := false
	cli, stdout, _ := testCLI(&fakeCommandApplication{})
	cli.OpenRuntime = func(context.Context, Settings) (CommandApplication, error) {
		applicationOpened = true
		return &fakeCommandApplication{}, nil
	}
	cli.CheckRuntime = func(Settings) DoctorReport {
		usage := 97.1
		return DoctorReport{Healthy: false, DiskUsage: &usage, Checks: []DoctorCheck{{Name: "disk_usage", Status: CheckFail, Message: "disk usage is 97.1%"}}}
	}
	code := cli.Run(context.Background(), []string{"run-daily", "--dry-run", "--json"})
	if code != ExitFailure {
		t.Fatalf("exit code = %d, want %d", code, ExitFailure)
	}
	if applicationOpened {
		t.Fatal("runtime opened the target despite failed preflight")
	}
	if !strings.Contains(stdout.String(), `"fatal":true`) || !strings.Contains(stdout.String(), `"status":"failed"`) {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestCLIRunDailyDryRunUsesMemoryAndDoesNotCreateTarget(t *testing.T) {
	directory := t.TempDir()
	targetDatabase := filepath.Join(directory, "must-not-exist.db")
	discoveryPath := filepath.Join(directory, "discovery.yaml")
	topicsPath := filepath.Join(directory, "topics.yaml")
	writeFixture(t, discoveryPath, fmt.Sprintf(`version: 1
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
profiles: []
manual:
  files: []
legacy:
  database_path: ""
`))
	writeFixture(t, topicsPath, `version: 1
topics:
  - slug: agents
    name: Agents
    status: active
`)
	healthy := func(Settings) DoctorReport {
		usage := 20.0
		return DoctorReport{Healthy: true, DiskUsage: &usage}
	}
	stdout := &bytes.Buffer{}
	cli := &CLI{
		Stdout: stdout,
		Stderr: &bytes.Buffer{},
		LoadSettings: func() (Settings, error) {
			return Settings{DatabasePath: targetDatabase, DiscoveryConfig: discoveryPath, TopicsConfig: topicsPath}, nil
		},
		CheckRuntime: healthy,
		OpenRuntime: func(ctx context.Context, settings Settings) (CommandApplication, error) {
			t.Fatal("dry-run used the writable runtime")
			return nil, nil
		},
		OpenPlanningRuntime: func(ctx context.Context, settings Settings) (CommandApplication, error) {
			if settings.DatabasePath != targetDatabase {
				t.Fatalf("planning target = %q", settings.DatabasePath)
			}
			return OpenRuntimeWithOptions(ctx, settings, RuntimeOptions{
				Now:          func() time.Time { return time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) },
				CheckRuntime: healthy,
				ReadOnly:     true,
			})
		},
	}
	if code := cli.Run(context.Background(), []string{"run-daily", "--dry-run", "--json"}); code != ExitSuccess {
		t.Fatalf("exit code = %d, output = %s", code, stdout.String())
	}
	if _, err := os.Stat(targetDatabase); !os.IsNotExist(err) {
		t.Fatalf("dry-run created target database: %v", err)
	}
}

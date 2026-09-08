package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

const validBatchProjectJSON = `{"full_name":"owner/repo","repository_id":12,"summary_zh":"摘要","key_points":["能力"],"use_cases":["场景"],"source":"legacy_research","model":"fixture-model","analyzed_at":"2026-09-08T10:30:00+08:00"}`

func TestDecodeAnalysisBatchRejectsUnknownAmbiguousAndMalformedInput(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"empty-array", `{"projects":[]}`},
		{"null-array", `{"projects":null}`},
		{"null-item", `{"projects":[null]}`},
		{"unknown-top-field", `{"projects":[` + validBatchProjectJSON + `],"overwrite":true}`},
		{"unknown-project-field", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"model":`, `"overwrite":true,"model":`, 1) + `]}`},
		{"duplicate-field", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"model":`, `"source":"manual","model":`, 1) + `]}`},
		{"duplicate-case-field", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"model":`, `"SOURCE":"manual","model":`, 1) + `]}`},
		{"duplicate-project", `{"projects":[` + validBatchProjectJSON + `,` + strings.ReplaceAll(validBatchProjectJSON, "owner/repo", "OWNER/repo") + `]}`},
		{"invalid-time", `{"projects":[` + strings.ReplaceAll(validBatchProjectJSON, "2026-09-08T10:30:00+08:00", "2026-02-30T10:30:00Z") + `]}`},
		{"null-time", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"2026-09-08T10:30:00+08:00"`, `null`, 1) + `]}`},
		{"non-string-model", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"fixture-model"`, `5`, 1) + `]}`},
		{"null-model", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"fixture-model"`, `null`, 1) + `]}`},
		{"non-string-list-member", `{"projects":[` + strings.Replace(validBatchProjectJSON, `["能力"]`, `[7]`, 1) + `]}`},
		{"null-list-member", `{"projects":[` + strings.Replace(validBatchProjectJSON, `["能力"]`, `[null]`, 1) + `]}`},
		{"null-list", `{"projects":[` + strings.Replace(validBatchProjectJSON, `["场景"]`, `null`, 1) + `]}`},
		{"empty-source", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"legacy_research"`, `" "`, 1) + `]}`},
		{"zero-id", `{"projects":[` + strings.Replace(validBatchProjectJSON, `"repository_id":12`, `"repository_id":0`, 1) + `]}`},
		{"trailing-object", `{"projects":[` + validBatchProjectJSON + `]} {}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeAnalysisBatch([]byte(test.body)); err == nil {
				t.Fatal("malformed batch was accepted")
			}
		})
	}
	projects, err := decodeAnalysisBatch([]byte(`{"projects":[` + validBatchProjectJSON + `]}`))
	if err != nil || len(projects) != 1 || projects[0].RepositoryID == nil || *projects[0].RepositoryID != 12 || projects[0].Model != "fixture-model" || projects[0].AnalyzedAt.UTC().Format("2006-01-02T15:04:05Z") != "2026-09-08T02:30:00Z" {
		t.Fatalf("valid batch = %+v, %v", projects, err)
	}
	minimal, err := decodeAnalysisBatch([]byte(`{"projects":[{"full_name":"owner/repo","summary_zh":"摘要","source":"codex"}]}`))
	if err != nil || len(minimal) != 1 || !minimal[0].AnalyzedAt.IsZero() || minimal[0].RepositoryID != nil {
		t.Fatalf("optional fields = %+v, %v", minimal, err)
	}
}

func TestDecodeAnalysisBatchEnforcesProjectLimitWhileDecoding(t *testing.T) {
	var contents strings.Builder
	contents.WriteString(`{"projects":[`)
	for index := 1; index <= 5000; index++ {
		if index > 1 {
			contents.WriteByte(',')
		}
		fmt.Fprintf(&contents, `{"full_name":"owner/repo-%d","summary_zh":"摘要","source":"codex"}`, index)
	}
	valid := contents.String() + `]}`
	if projects, err := decodeAnalysisBatch([]byte(valid)); err != nil || len(projects) != 5000 {
		t.Fatalf("maximum-sized valid batch: count=%d, err=%v", len(projects), err)
	}
	tooMany := contents.String() + `,{"full_name":"owner/last","summary_zh":"摘要","source":"codex"}]}`
	if _, err := decodeAnalysisBatch([]byte(tooMany)); err == nil {
		t.Fatal("5001 entries accepted")
	}
}

type batchCommandFake struct {
	*fakeCommandApplication
	projects []domain.AnalysisImportProject
	calls    int
}

func (application *batchCommandFake) ImportAnalysisBatch(_ context.Context, projects []domain.AnalysisImportProject) (domain.AnalysisBatchResult, error) {
	application.projects = projects
	application.calls++
	return domain.AnalysisBatchResult{Imported: 1, Skipped: len(projects) - 1}, nil
}

func TestCLIAnalysisBatchUsesOneRuntimeInvocationAndRejectsFilesBeforeOpening(t *testing.T) {
	application := &batchCommandFake{fakeCommandApplication: &fakeCommandApplication{}}
	cli, stdout, _ := testCLI(application.fakeCommandApplication)
	opened := 0
	cli.OpenRuntime = func(context.Context, Settings) (CommandApplication, error) { opened++; return application, nil }
	path := filepath.Join(t.TempDir(), "batch.json")
	writeFixture(t, path, `{"projects":[`+validBatchProjectJSON+`]}`)
	if code := cli.Run(context.Background(), []string{"analysis", "import-batch", "--file", path, "--json"}); code != ExitSuccess {
		t.Fatalf("CLI exit=%d output=%s", code, stdout.String())
	}
	var response struct {
		Status  string
		Command string
		Result  domain.AnalysisBatchResult
	}
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil || response.Status != "success" || response.Command != "analysis import-batch" || response.Result.Imported != 1 || application.calls != 1 || opened != 1 || !application.closed {
		t.Fatalf("batch CLI response=%+v err=%v calls=%d opened=%d", response, err, application.calls, opened)
	}
	stdout.Reset()
	writeFixture(t, path, `{"projects":[`+validBatchProjectJSON+`],"overwrite":true}`)
	if code := cli.Run(context.Background(), []string{"analysis", "import-batch", "--file", path, "--json"}); code != ExitUsage || opened != 1 {
		t.Fatalf("invalid file opened runtime: code=%d opened=%d", code, opened)
	}
}

func TestCLIAnalysisBatchCommitsOnceAndRollsBackIdentityErrors(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	databasePath := filepath.Join(directory, "batch.db")
	database, err := sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	for id, name := range []string{"owner/first", "owner/second"} {
		if _, _, err := database.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: int64(id + 1), FullName: name, Source: domain.DiscoverySourceLegacy,
			DiscoveredAt: time.Date(2026, 9, 8, 1, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	cli := NewCLI()
	var stdout, stderr bytes.Buffer
	cli.Stdout, cli.Stderr = &stdout, &stderr
	cli.LoadSettings = func() (Settings, error) { return Settings{DatabasePath: databasePath}, nil }
	path := filepath.Join(directory, "analyses.json")
	writeFixture(t, path, `{"projects":[{"full_name":"owner/first","summary_zh":"一","source":"legacy_research"},{"full_name":"owner/second","repository_id":999,"summary_zh":"二","source":"legacy_research"}]}`)
	if code := cli.Run(ctx, []string{"analysis", "import-batch", "--file", path, "--json"}); code != ExitFailure {
		t.Fatalf("identity mismatch code=%d output=%s", code, stdout.String())
	}
	database, err = sqlite.Open(ctx, databasePath)
	if err != nil {
		t.Fatal(err)
	}
	detail, err := database.GetRepositoryDetail(ctx, 1, domain.Date("2026-09-08"))
	if err != nil || detail.Analysis != nil {
		t.Fatalf("CLI batch left a partial import: %+v, %v", detail.Analysis, err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, strings.Replace(string(contents), `"repository_id":999`, `"repository_id":2`, 1))
	stdout.Reset()
	if code := cli.Run(ctx, []string{"analysis", "import-batch", "--file", path}); code != ExitSuccess || !strings.Contains(stdout.String(), "Imported 2 project analyses; skipped 0") {
		t.Fatalf("valid CLI batch failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestAnalysisBatchUnsupportedRuntimeDoesNotChangeSingleImport(t *testing.T) {
	application := &fakeCommandApplication{}
	cli, _, _ := testCLI(application)
	path := filepath.Join(t.TempDir(), "batch.json")
	writeFixture(t, path, `{"projects":[`+validBatchProjectJSON+`]}`)
	if code := cli.Run(context.Background(), []string{"analysis", "import-batch", "--file", path}); code != ExitFailure || !application.closed {
		t.Fatal("old runtime without optional batch support was not handled")
	}
}

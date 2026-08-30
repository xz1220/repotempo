package csv

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestFeishuBridgeMapsFieldsByNameAndEpochDate(t *testing.T) {
	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("jq is required for the Feishu bridge")
	}
	directory := t.TempDir()
	lark := filepath.Join(directory, "lark-mock")
	gh := filepath.Join(directory, "gh-mock")
	output := filepath.Join(directory, "base.csv")
	writeExecutable(t, lark, `#!/bin/sh
case "$*" in
  *"--table-id project"*)
    printf '%s\n' '{"data":{"fields":["累计 Stars","人工收藏","项目","最后观察","语言","首次出现","分类","GitHub"],"data":[[120,true,"owner/repo",1788019200000,"Go",1787932800000,["Agent 与多智能体"],"https://github.com/owner/repo"]],"has_more":false}}'
    ;;
  *"--table-id daily"*)
    printf '%s\n' '{"data":{"fields":["今日排名","累计 Stars","项目","日期","GitHub"],"data":[[3,123,"owner/repo",1788019200000,"https://github.com/owner/repo"]],"has_more":false}}'
    ;;
  *) exit 1 ;;
esac
`)
	writeExecutable(t, gh, `#!/bin/sh
printf '%s\n' '{"id":42,"html_url":"https://github.com/owner/repo"}'
`)
	repositoryRoot := filepath.Join("..", "..", "..")
	command := exec.Command("sh", filepath.Join(repositoryRoot, "scripts", "export-feishu-base.sh"), "base", "project", "daily", output)
	command.Env = append(os.Environ(), "LARK_CLI_BIN="+lark, "LARK_CLI_NODE=", "GH_BIN="+gh, "JQ_BIN="+jq)
	if combined, err := command.CombinedOutput(); err != nil {
		t.Fatalf("bridge failed: %v\n%s", err, combined)
	}
	file, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	result, err := Import(file, Options{Location: time.FixedZone("CST", 8*60*60)})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || result.Candidates[0].Repository.ID != 42 || result.Candidates[0].Repository.Language != "Go" {
		t.Fatalf("candidates = %#v", result.Candidates)
	}
	if len(result.Observations) != 1 || result.Observations[0].StarCount != 123 || result.Observations[0].Metadata["oss_today_rank"] != "3" {
		t.Fatalf("observations = %#v", result.Observations)
	}
	if result.Observations[0].SnapshotDate.Format("2006-01-02") == "1970-01-01" {
		t.Fatalf("epoch milliseconds were not normalized: %#v", result.Observations[0])
	}
}

func writeExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o700); err != nil {
		t.Fatal(err)
	}
}

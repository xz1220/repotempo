package app

import (
	"context"
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
)

func TestTagsImportJSONIsStrictAndRetainsOmittedVsEmptyFields(t *testing.T) {
	contents := []byte(`{"projects":[{"repository_id":1,"full_name":"o/unknown","research_tags":[" 中文写作 ","RAG","rag"]},{"repository_id":2,"full_name":"o/empty","github_topics":[]}]}`)
	projects, err := decodeTagsImport(contents)
	if err != nil || len(projects) != 2 || projects[0].GitHubTopics != nil || !reflect.DeepEqual(*projects[0].ResearchTags, []string{"中文写作", "rag"}) || projects[1].GitHubTopics == nil || *projects[1].GitHubTopics == nil {
		t.Fatalf("decoded tags=%v err=%v", projects, err)
	}
	for _, raw := range []string{
		`{"projects":[]}`, `{"projects":null}`,
		`{"projects":[{"repository_id":0,"full_name":"o/a","github_topics":[]}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","github_topics":null}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","research_tags":[null]}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","github_topics":[5]}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","research_tags":["internal\ncontrol"]}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","github_topics":[],"topic":"wrong-classification"}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a","github_topics":[],"github_topics":["duplicate-field"]}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a"},{"repository_id":1,"full_name":"o/a"}]}`,
		`{"projects":[{"repository_id":1,"full_name":"o/a"}],"overwrite":true}`,
	} {
		if _, err := decodeTagsImport([]byte(raw)); err == nil {
			t.Fatalf("invalid tags JSON accepted: %s", raw)
		}
	}
}

type tagsCommandFake struct {
	*fakeCommandApplication
	calls    int
	projects []domain.RepositoryTagsImport
}

func (application *tagsCommandFake) ImportTagsBatch(_ context.Context, projects []domain.RepositoryTagsImport) (domain.TagsBatchResult, error) {
	application.calls++
	application.projects = projects
	return domain.TagsBatchResult{Updated: 1, GitHubTopicsFilled: 1}, nil
}

func TestTagsImportCLIUsesOneOptionalBatchOperation(t *testing.T) {
	for _, action := range []string{"import", "import-batch"} {
		t.Run(action, func(t *testing.T) {
			application := &tagsCommandFake{fakeCommandApplication: &fakeCommandApplication{}}
			cli, stdout, _ := testCLI(application.fakeCommandApplication)
			opened := 0
			cli.OpenRuntime = func(context.Context, Settings) (CommandApplication, error) { opened++; return application, nil }
			path := filepath.Join(t.TempDir(), "tags.json")
			writeFixture(t, path, `{"projects":[{"repository_id":12,"full_name":"owner/repo","github_topics":[]}]}`)
			if code := cli.Run(context.Background(), []string{"tags", action, "--file", path, "--json"}); code != ExitSuccess {
				t.Fatalf("code=%d output=%s", code, stdout.String())
			}
			var output struct {
				Result domain.TagsBatchResult
				Status string
			}
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil || output.Status != "success" || output.Result.GitHubTopicsFilled != 1 || application.calls != 1 || opened != 1 || !application.closed {
				t.Fatalf("output=%+v err=%v", output, err)
			}
			writeFixture(t, path, `{"projects":[{"repository_id":12,"full_name":"owner/repo","github_topics":[null]}]}`)
			if code := cli.Run(context.Background(), []string{"tags", action, "--file", path, "--json"}); code != ExitUsage || opened != 1 {
				t.Fatal("invalid tag file opened a writable runtime")
			}
		})
	}
}

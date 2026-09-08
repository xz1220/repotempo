package csv

import (
	"bytes"
	standardcsv "encoding/csv"
	"reflect"
	"testing"
)

func TestImportCSVSeparatesRawTopicsFromTaxonomyAndResearch(t *testing.T) {
	var input bytes.Buffer
	writer := standardcsv.NewWriter(&input)
	if err := writer.WriteAll([][]string{
		{"github_repo_id", "full_name", "topics", "github_topics", "research_tags"},
		{"1", "owner/unknown", "AI Agent", "", ""},
		{"2", "owner/empty", "AI Agent", "[]", `["中文写作"]`},
		{"3", "owner/tagged", "AI Agent", `["golang"]`, `["开发工具"]`},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := Import(&input, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 3 || len(result.Warnings) != 0 {
		t.Fatalf("result = %+v", result)
	}
	unknown := result.Candidates[0].Repository
	empty := result.Candidates[1].Repository
	tagged := result.Candidates[2].Repository
	if unknown.Topics != nil || unknown.ResearchTags != nil {
		t.Fatalf("taxonomy must not masquerade as raw metadata: %+v", unknown)
	}
	if empty.Topics == nil || len(empty.Topics) != 0 || !reflect.DeepEqual(empty.ResearchTags, []string{"中文写作"}) {
		t.Fatalf("explicit empty and research topics lost: %+v", empty)
	}
	if !reflect.DeepEqual(tagged.Topics, []string{"golang"}) || !reflect.DeepEqual(tagged.ResearchTags, []string{"开发工具"}) {
		t.Fatalf("topic provenance mixed: %+v", tagged)
	}
}

package source

import (
	"reflect"
	"testing"
)

func TestCandidateTopicsPreserveUnknownEmptyAndAuthoritativeReplacement(t *testing.T) {
	for _, test := range []struct {
		name                  string
		old, next             []string
		oldSource, nextSource string
		want                  []string
	}{
		{"unknown", nil, nil, "legacy", "github_search", nil},
		{"known-empty", nil, []string{}, "legacy", "github_search", []string{}},
		{"authoritative-clear", []string{"old"}, []string{}, "legacy", "github_search", []string{}},
		{"legacy-cannot-override-current", []string{"current"}, []string{"old"}, "github_search", "legacy", []string{"current"}},
		{"missing-cannot-erase", []string{"current"}, nil, "legacy", "github_search", []string{"current"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			merged := MergeCandidates([]Candidate{{Repository: Repository{ID: 1, FullName: "o/r", Topics: test.old}, Source: test.oldSource}, {Repository: Repository{ID: 1, FullName: "o/r", Topics: test.next}, Source: test.nextSource}})
			if len(merged) != 1 || !reflect.DeepEqual(merged[0].Repository.Topics, test.want) {
				t.Fatalf("topics=%v want=%v", merged, test.want)
			}
		})
	}
}

func TestParseTagsJSONRejectsNonStringsAndRetainsNullVsEmpty(t *testing.T) {
	unknown, err := ParseTagsJSON("null")
	if err != nil || unknown != nil {
		t.Fatal("null must remain unknown")
	}
	empty, err := ParseTagsJSON("[]")
	if err != nil || empty == nil || len(empty) != 0 {
		t.Fatal("empty array must remain known")
	}
	for _, raw := range []string{`[null]`, `[1]`, `[{}]`, `{"tag":"ai"}`, `not-json`} {
		if _, err := ParseTagsJSON(raw); err == nil {
			t.Fatalf("accepted invalid raw tags %s", raw)
		}
	}
}

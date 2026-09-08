package github

import (
	"encoding/json"
	"testing"
)

func TestGitHubPayloadPreservesMissingAndKnownEmptyTopics(t *testing.T) {
	for _, test := range []struct {
		raw   string
		known bool
	}{
		{`{"id":1,"full_name":"owner/repo","stargazers_count":10}`, false},
		{`{"id":1,"full_name":"owner/repo","stargazers_count":10,"topics":null}`, false},
		{`{"id":1,"full_name":"owner/repo","stargazers_count":10,"topics":[]}`, true},
	} {
		var payload githubRepository
		if err := json.Unmarshal([]byte(test.raw), &payload); err != nil {
			t.Fatal(err)
		}
		repository, err := payload.toSource()
		if err != nil || (repository.Topics != nil) != test.known {
			t.Fatalf("raw=%s topics=%v err=%v", test.raw, repository.Topics, err)
		}
	}
}

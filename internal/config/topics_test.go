package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadTopicsExampleAndFlatten(t *testing.T) {
	topics, err := LoadTopics(filepath.Join("..", "..", "config", "topics.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	flat := topics.Flatten()
	if got, want := len(flat), 10; got != want {
		t.Fatalf("flat topics = %d, want %d", got, want)
	}
	wantChildren := map[string]bool{
		"skills": true, "harness": true, "multi-agent": true, "memory": true,
		"browser-and-tools": true, "reliability": true, "workbench": true,
		"client-and-remote-access": true,
	}
	for _, topic := range flat {
		if wantChildren[topic.Slug] {
			if topic.ParentSlug != "ai-agent" {
				t.Fatalf("topic %s parent = %q, want ai-agent", topic.Slug, topic.ParentSlug)
			}
			delete(wantChildren, topic.Slug)
		}
	}
	if len(wantChildren) != 0 {
		t.Fatalf("missing child topics: %v", wantChildren)
	}
}

func TestDecodeTopicsRejectsUnknownField(t *testing.T) {
	raw := "version: 1\ntopics:\n  - slug: root\n    name: Root\n    status: active\n    mystery: true\n"
	_, err := DecodeTopics(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "field mystery not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeTopicsRejectsDuplicateSlug(t *testing.T) {
	raw := "version: 1\ntopics:\n  - slug: same\n    name: One\n    status: active\n  - slug: same\n    name: Two\n    status: active\n"
	_, err := DecodeTopics(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "duplicate topic slug") {
		t.Fatalf("error = %v", err)
	}
}

func TestDecodeTopicsRejectsThirdLevel(t *testing.T) {
	raw := `version: 1
topics:
  - slug: root
    name: Root
    status: active
    children:
      - slug: child
        name: Child
        status: active
        children:
          - slug: grandchild
            name: Grandchild
            status: active
`
	_, err := DecodeTopics(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "two-level topic limit") {
		t.Fatalf("error = %v", err)
	}
}

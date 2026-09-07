package classification_test

import (
	"context"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/classification"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

func TestProductClassificationUsesExplicitEvidence(t *testing.T) {
	for _, test := range []struct {
		name, description string
		topics, want      []string
	}{
		{"openai/codex", "Lightweight coding agent that runs in your terminal", nil, []string{"coding-agents"}},
		{"team/research", "AI-powered deep research assistant", nil, []string{"research-agents"}},
		{"browser-use/browser-use", "Make websites accessible for AI agents", []string{"browser-use"}, []string{"browser-computer-agents"}},
		{"team/flow", "LLM workflow automation platform", nil, []string{"workflow-agents"}},
		{"team/framework", "Build multi-agent LLM systems", nil, []string{"agent-platforms", "multi-agent"}},
		{"mem0ai/mem0", "Memory layer for AI agents", []string{"agent-memory"}, []string{"ai-infrastructure", "memory"}},
		{"team/skills", "Reusable agent skills for Claude", []string{"skills"}, []string{"ai-infrastructure", "skills"}},
		{"team/agent", "A general-purpose AI agent", []string{"ai-agent"}, []string{"ai-agent"}},
		{"Ebazhanov/linkedin-skill-assessments-quizzes", "LinkedIn skills assessment answers", []string{"skills"}, nil},
		{"team/awesome-agents", "Curated list of AI agents", []string{"ai-agent", "agent-skills"}, nil},
		{"team/browser-library", "A browser automation library", []string{"browser-automation"}, nil},
		{"team/monitoring-agent", "Monitoring agent for servers", []string{"agent"}, nil},
		{"team/rag", "An AI framework for building retrieval pipelines", nil, nil},
		{"team/new-coder", "The open source coding agent.", nil, []string{"coding-agents"}},
		{"team/agent-workspace", "A workspace for a fleet of parallel agents. Run any coding agent.", nil, []string{"agent-platforms", "multi-agent"}},
		{"team/server-fleet", "A fleet of parallel agents monitoring servers", nil, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := classification.Infer(test.name, test.description, test.topics)
			if len(got) != len(test.want) {
				t.Fatalf("got %+v, want %v", got, test.want)
			}
			for i, match := range got {
				if match.Slug != test.want[i] || match.Reason == "" {
					t.Fatalf("got %+v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestCuratedBenchmarksUseExactRepositoryIdentity(t *testing.T) {
	for _, test := range []struct {
		name, description string
		want              []string
	}{
		{"anomalyco/opencode", "The open source coding agent.", []string{"coding-agents"}},
		{"langchain-ai/langgraph", "Build resilient agents.", []string{"agent-platforms"}},
		{"browser-use/browser-use", "Make websites accessible for AI agents. Automate tasks online with ease.", []string{"browser-computer-agents"}},
		{"stablyai/orca", "Orca is the ADE for working with a fleet of parallel agents. Run any coding agent with your own subscription. Available on desktop, mobile and remote runtime.", []string{"agent-platforms", "client-and-remote-access", "multi-agent", "workbench"}},
		{"deepseek-ai/deepseek-harness", "DeepSeek Harness: Everything is a Plugin.", []string{"ai-infrastructure", "harness"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			matches := classification.Infer(test.name, test.description, nil)
			if len(matches) != len(test.want) {
				t.Fatalf("got %+v, want %v", matches, test.want)
			}
			for i, match := range matches {
				if match.Slug != test.want[i] || match.Confidence != .98 {
					t.Fatalf("unexpected benchmark: %+v", matches)
				}
			}
		})
	}
	for _, fullName := range []string{"other/opencode", "other/langgraph", "other/browser-use", "other/orca", "other/deepseek-harness", "stablyai/orca-guide"} {
		if got := classification.Infer(fullName, "A generic project", nil); len(got) != 0 {
			t.Fatalf("name-only false positive for %s: %+v", fullName, got)
		}
	}
	if got := classification.Infer("anomalyco/opencode", "An interview question-bank and curated list", nil); len(got) != 0 {
		t.Fatalf("curation bypassed resource exclusion: %+v", got)
	}
}

func TestApplyProtectsManualAssignmentsAndVetoes(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repo, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 7, FullName: "anomalyco/opencode", Source: domain.DiscoverySourceManual, DiscoveredAt: time.Now(), Description: str("The open source coding agent.")})
	if err != nil {
		t.Fatal(err)
	}
	topic, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "coding-agents", Name: "Coding Agents"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveRepositoryTopic(ctx, repo.GitHubRepoID, topic.ID, domain.TopicSourceManual); err != nil {
		t.Fatal(err)
	}
	changed, err := classification.Apply(ctx, store, repo, nil, time.Now())
	if err != nil || changed != 0 {
		t.Fatalf("veto overwritten: %d, %v", changed, err)
	}
	visible, err := store.ListRepositoryTopics(ctx, repo.GitHubRepoID)
	if err != nil || len(visible) != 0 {
		t.Fatalf("veto visible: %+v %v", visible, err)
	}
	one := 1.0
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: repo.GitHubRepoID, TopicID: topic.ID, Source: domain.TopicSourceManual, Confidence: &one}); err != nil {
		t.Fatal(err)
	}
	changed, err = classification.Apply(ctx, store, repo, nil, time.Now())
	if err != nil || changed != 0 {
		t.Fatalf("manual assignment overwritten: %d, %v", changed, err)
	}
	assignments, err := store.ListRepositoryTopicAssignments(ctx, &repo.GitHubRepoID)
	if err != nil || assignments[0].Source != domain.TopicSourceManual {
		t.Fatalf("manual assignment changed: %+v %v", assignments, err)
	}
}

func str(value string) *string { return &value }

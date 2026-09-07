package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestOfflineReclassificationDryRunProtectsDatabaseAndManualVeto(t *testing.T) {
	ctx := context.Background()
	settings := Settings{DatabasePath: filepath.Join(t.TempDir(), "radar.db"), TopicsConfig: filepath.Join("..", "..", "config", "topics.example.yaml")}
	runtime, err := OpenRuntime(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	description := "AI coding assistant for your terminal"
	_, _, err = runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 31, FullName: "team/codex", Description: &description, Source: domain.DiscoverySourceManual, DiscoveredAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	planner, err := OpenPlanningRuntime(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := planner.ReclassifyTopics(ctx, true)
	if err != nil {
		t.Fatal(err)
	}
	if plan.RepositoryCount != 1 || plan.MatchedCount != 1 || plan.ChangedCount != 1 {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	if err := planner.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(settings.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("dry run modified the database")
	}
	runtime, err = OpenRuntime(ctx, settings)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	result, err := runtime.ReclassifyTopics(ctx, false)
	if err != nil || result.ChangedCount != 1 {
		t.Fatalf("apply = %+v %v", result, err)
	}
	topic, err := runtime.store.GetTopicBySlug(ctx, "coding-agents")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.store.RemoveRepositoryTopic(ctx, 31, topic.ID, domain.TopicSourceManual); err != nil {
		t.Fatal(err)
	}
	result, err = runtime.ReclassifyTopics(ctx, false)
	if err != nil || result.ProtectedCount != 1 || result.ChangedCount != 0 {
		t.Fatalf("protected apply = %+v %v", result, err)
	}
}

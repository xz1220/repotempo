package sqlite

import (
	"context"
	"testing"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestRepositoryCanHaveMultipleTopicsAndManualWins(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/project")

	root, created, err := store.UpsertTopic(ctx, domain.Topic{Slug: "ai-agent", Name: "AI Agent"})
	if err != nil || !created {
		t.Fatalf("create root topic = (%+v, %t, %v)", root, created, err)
	}
	child, created, err := store.UpsertTopic(ctx, domain.Topic{
		Slug:     "memory",
		Name:     "Memory",
		ParentID: &root.ID,
	})
	if err != nil || !created {
		t.Fatalf("create child topic = (%+v, %t, %v)", child, created, err)
	}
	other, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "reliability", Name: "Reliability", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create second child: %v", err)
	}
	if _, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "too-deep", Name: "Too Deep", ParentID: &child.ID}); err == nil {
		t.Fatal("creating a third topic level unexpectedly succeeded")
	}

	manual, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
		RepositoryID: 1,
		TopicID:      child.ID,
		Source:       domain.TopicSourceManual,
		Confidence:   pointer(1.0),
	})
	if err != nil || !manual.Changed || !manual.Assignment.Confirmed {
		t.Fatalf("manual assignment = (%+v, %v)", manual, err)
	}
	auto, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
		RepositoryID: 1,
		TopicID:      child.ID,
		Source:       domain.TopicSourceAuto,
		Confidence:   pointer(0.25),
	})
	if err != nil || !auto.Protected || auto.Assignment.Source != domain.TopicSourceManual {
		t.Fatalf("automatic overwrite = (%+v, %v), want protected manual", auto, err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
		RepositoryID: 1,
		TopicID:      other.ID,
		Source:       domain.TopicSourceGitHub,
		Confidence:   pointer(0.8),
	}); err != nil {
		t.Fatalf("assign second topic: %v", err)
	}
	topics, err := store.ListRepositoryTopics(ctx, 1)
	if err != nil {
		t.Fatalf("list repository topics: %v", err)
	}
	if len(topics) != 2 {
		t.Fatalf("topic count = %d, want 2", len(topics))
	}
	assignments, err := store.ListRepositoryTopicAssignments(ctx, nil)
	if err != nil || len(assignments) != 2 {
		t.Fatalf("assignments = (%+v, %v)", assignments, err)
	}
	removed, err := store.RemoveRepositoryTopic(ctx, 1, child.ID, domain.TopicSourceAuto)
	if err != nil || removed {
		t.Fatalf("automatic removal of manual topic = (%t, %v)", removed, err)
	}
	removed, err = store.RemoveRepositoryTopic(ctx, 1, child.ID, domain.TopicSourceManual)
	if err != nil || !removed {
		t.Fatalf("manual topic removal = (%t, %v)", removed, err)
	}
}

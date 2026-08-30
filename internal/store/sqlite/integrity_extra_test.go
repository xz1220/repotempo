package sqlite

import (
	"context"
	"strings"
	"testing"

	"github.com/xz1220/github-radar/internal/domain"
)

func TestRepositoryDeleteCascadesSnapshotsAndTopicAssignments(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	addRepository(t, store, 1, "owner/deleted")
	addRepository(t, store, 2, "owner/retained")
	putSuccess(t, store, 1, "2026-08-30", 10)
	putSuccess(t, store, 2, "2026-08-30", 20)

	topic, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "database", Name: "Database"})
	if err != nil {
		t.Fatalf("create topic: %v", err)
	}
	for _, repositoryID := range []int64{1, 2} {
		if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
			RepositoryID: repositoryID,
			TopicID:      topic.ID,
			Source:       domain.TopicSourceAuto,
		}); err != nil {
			t.Fatalf("assign topic to repository %d: %v", repositoryID, err)
		}
	}

	result, err := store.db.ExecContext(ctx, "DELETE FROM repositories WHERE github_repo_id = ?", 1)
	if err != nil {
		t.Fatalf("delete repository: %v", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		t.Fatalf("read repository delete result: %v", err)
	}
	if affected != 1 {
		t.Fatalf("deleted repository rows = %d, want 1", affected)
	}

	for _, table := range []string{"daily_snapshots", "repository_topics"} {
		var deletedRows int
		query := "SELECT COUNT(*) FROM " + table + " WHERE repository_id = ?"
		if err := store.db.QueryRowContext(ctx, query, 1).Scan(&deletedRows); err != nil {
			t.Fatalf("count deleted repository rows in %s: %v", table, err)
		}
		if deletedRows != 0 {
			t.Fatalf("%s rows for deleted repository = %d, want 0", table, deletedRows)
		}

		var retainedRows int
		if err := store.db.QueryRowContext(ctx, query, 2).Scan(&retainedRows); err != nil {
			t.Fatalf("count retained repository rows in %s: %v", table, err)
		}
		if retainedRows != 1 {
			t.Fatalf("%s rows for retained repository = %d, want 1", table, retainedRows)
		}
	}

	if _, err := store.GetTopic(ctx, topic.ID); err != nil {
		t.Fatalf("repository deletion removed shared topic: %v", err)
	}
}

func TestTopicUpdateTriggerEnforcesTwoLevelReparenting(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()

	rootA, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "root-a", Name: "Root A"})
	if err != nil {
		t.Fatalf("create root A: %v", err)
	}
	rootB, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "root-b", Name: "Root B"})
	if err != nil {
		t.Fatalf("create root B: %v", err)
	}
	child, _, err := store.UpsertTopic(ctx, domain.Topic{
		Slug:     "child",
		Name:     "Child",
		ParentID: &rootA.ID,
	})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}

	if _, err := store.db.ExecContext(ctx,
		"UPDATE topics SET parent_id = ? WHERE id = ?", rootB.ID, child.ID,
	); err != nil {
		t.Fatalf("reparent leaf between roots: %v", err)
	}
	assertTopicParent(t, store, child.ID, &rootB.ID)

	invalidUpdates := []struct {
		name     string
		topicID  int64
		parentID int64
	}{
		{
			name:     "root with a child cannot become a child",
			topicID:  rootB.ID,
			parentID: rootA.ID,
		},
		{
			name:     "topic cannot become a child of a child",
			topicID:  rootA.ID,
			parentID: child.ID,
		},
	}
	for _, test := range invalidUpdates {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.db.ExecContext(ctx,
				"UPDATE topics SET parent_id = ? WHERE id = ?", test.parentID, test.topicID,
			)
			if err == nil {
				t.Fatal("three-level reparent unexpectedly succeeded")
			}
			if !strings.Contains(err.Error(), "topic hierarchy supports at most two levels") {
				t.Fatalf("reparent error = %v, want two-level hierarchy error", err)
			}
		})
	}

	assertTopicParent(t, store, rootA.ID, nil)
	assertTopicParent(t, store, rootB.ID, nil)
	assertTopicParent(t, store, child.ID, &rootB.ID)
}

func assertTopicParent(t *testing.T, store *Store, topicID int64, want *int64) {
	t.Helper()
	topic, err := store.GetTopic(context.Background(), topicID)
	if err != nil {
		t.Fatalf("get topic %d: %v", topicID, err)
	}
	if want == nil {
		if topic.ParentID != nil {
			t.Fatalf("topic %d parent = %d, want nil", topicID, *topic.ParentID)
		}
		return
	}
	if topic.ParentID == nil || *topic.ParentID != *want {
		t.Fatalf("topic %d parent = %v, want %d", topicID, topic.ParentID, *want)
	}
}

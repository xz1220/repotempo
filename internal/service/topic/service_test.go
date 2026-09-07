package topic

import (
	"context"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

type fakeStore struct {
	assigned int
	removed  int
}

func (store *fakeStore) GetRepositoryByFullName(context.Context, string) (domain.Repository, error) {
	return domain.Repository{GitHubRepoID: 42, FullName: "owner/repo"}, nil
}

func (store *fakeStore) GetTopicBySlug(context.Context, string) (domain.Topic, error) {
	return domain.Topic{ID: 7, Slug: "agent-memory"}, nil
}

func (store *fakeStore) ListTopics(context.Context, domain.TopicStatus) ([]domain.Topic, error) {
	return []domain.Topic{{ID: 7, Slug: "agent-memory"}}, nil
}

func (store *fakeStore) AssignRepositoryTopic(_ context.Context, assignment domain.RepositoryTopic) (domain.TopicAssignmentResult, error) {
	store.assigned++
	return domain.TopicAssignmentResult{Assignment: assignment, Changed: true}, nil
}

func (store *fakeStore) RemoveRepositoryTopic(context.Context, int64, int64, domain.TopicSource) (bool, error) {
	store.removed++
	return true, nil
}

func TestManualAssignmentIsConfirmedAndDryRunDoesNotWrite(t *testing.T) {
	store := &fakeStore{}
	now := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	service := Service{Store: store, Now: func() time.Time { return now }}
	result, err := service.Assign(context.Background(), "owner/repo", "agent-memory", true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Assignment.Confirmed || result.Assignment.Source != domain.TopicSourceManual {
		t.Fatalf("unexpected assignment: %#v", result.Assignment)
	}
	if store.assigned != 0 {
		t.Fatal("dry run wrote assignment")
	}
}

func TestAssignAndRemove(t *testing.T) {
	store := &fakeStore{}
	service := Service{Store: store}
	if _, err := service.Assign(context.Background(), "owner/repo", "agent-memory", false); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Remove(context.Background(), "owner/repo", "agent-memory", false); err != nil {
		t.Fatal(err)
	}
	if store.assigned != 1 || store.removed != 1 {
		t.Fatalf("assigned=%d removed=%d", store.assigned, store.removed)
	}
}

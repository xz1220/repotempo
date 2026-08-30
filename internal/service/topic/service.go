package topic

import (
	"context"
	"fmt"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
)

type Store interface {
	GetRepositoryByFullName(context.Context, string) (domain.Repository, error)
	GetTopicBySlug(context.Context, string) (domain.Topic, error)
	ListTopics(context.Context, domain.TopicStatus) ([]domain.Topic, error)
	AssignRepositoryTopic(context.Context, domain.RepositoryTopic) (domain.TopicAssignmentResult, error)
	RemoveRepositoryTopic(context.Context, int64, int64, domain.TopicSource) (bool, error)
}

type Service struct {
	Store Store
	Now   func() time.Time
}

func (service Service) List(ctx context.Context) ([]domain.Topic, error) {
	if service.Store == nil {
		return nil, fmt.Errorf("topic service requires a store")
	}
	return service.Store.ListTopics(ctx, domain.TopicActive)
}

func (service Service) Assign(ctx context.Context, fullName, slug string, dryRun bool) (domain.TopicAssignmentResult, error) {
	repository, topic, err := service.resolve(ctx, fullName, slug)
	if err != nil {
		return domain.TopicAssignmentResult{}, err
	}
	now := service.now().UTC()
	confidence := 1.0
	assignment := domain.RepositoryTopic{
		RepositoryID: repository.GitHubRepoID,
		TopicID:      topic.ID,
		Source:       domain.TopicSourceManual,
		Confirmed:    true,
		Confidence:   &confidence,
		AssignedAt:   now,
		UpdatedAt:    now,
	}
	if dryRun {
		return domain.TopicAssignmentResult{Assignment: assignment, Changed: true}, nil
	}
	return service.Store.AssignRepositoryTopic(ctx, assignment)
}

func (service Service) Remove(ctx context.Context, fullName, slug string, dryRun bool) (bool, error) {
	repository, topic, err := service.resolve(ctx, fullName, slug)
	if err != nil {
		return false, err
	}
	if dryRun {
		return true, nil
	}
	return service.Store.RemoveRepositoryTopic(ctx, repository.GitHubRepoID, topic.ID, domain.TopicSourceManual)
}

func (service Service) resolve(ctx context.Context, fullName, slug string) (domain.Repository, domain.Topic, error) {
	if service.Store == nil {
		return domain.Repository{}, domain.Topic{}, fmt.Errorf("topic service requires a store")
	}
	repository, err := service.Store.GetRepositoryByFullName(ctx, fullName)
	if err != nil {
		return domain.Repository{}, domain.Topic{}, fmt.Errorf("resolve repository %q: %w", fullName, err)
	}
	topic, err := service.Store.GetTopicBySlug(ctx, slug)
	if err != nil {
		return domain.Repository{}, domain.Topic{}, fmt.Errorf("resolve topic %q: %w", slug, err)
	}
	return repository, topic, nil
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

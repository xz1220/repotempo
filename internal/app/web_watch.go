package app

import (
	"context"
	"errors"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/service/classification"
	"github.com/xz1220/github-radar/internal/service/watch"
	"github.com/xz1220/github-radar/internal/source/github"
	corestore "github.com/xz1220/github-radar/internal/store"
	"github.com/xz1220/github-radar/internal/web"
)

var _ web.Watcher = (*Runtime)(nil)

func (runtime *Runtime) WatchTopics(ctx context.Context) ([]web.TopicRef, error) {
	topics, err := runtime.store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return nil, err
	}
	return mapTopicFilterRefs(topics), nil
}

func (runtime *Runtime) AddWatch(ctx context.Context, request web.WatchRequest) (web.WatchResult, error) {
	if _, err := watch.NormalizeRepository(request.Repository); err != nil {
		return web.WatchResult{}, web.ErrWatchInvalid
	}
	_, client, _, err := runtime.dependencies()
	if err != nil {
		return web.WatchResult{}, web.ErrWatchUnavailable
	}
	service := watch.Service{Store: runtime.store, Resolver: client, Now: runtime.now}
	result, err := service.AddTracked(ctx, request.Repository, request.Note, request.TopicSlug)
	if err != nil {
		return web.WatchResult{}, mapWatchError(err)
	}
	if request.TopicSlug == "" {
		if _, err := classification.Apply(ctx, runtime.store, result.Repository, result.GitHubTopics, runtime.now()); err != nil {
			// The repository and its snapshot are already durable. Classification
			// can be retried by the collector without misreporting the add as failed.
			runtime.logger.WarnContext(ctx, "watch project classification deferred", "repository_id", result.Repository.GitHubRepoID)
		}
	}
	return web.WatchResult{ID: result.Repository.GitHubRepoID, FullName: result.Repository.FullName, Created: result.Created}, nil
}

// Return only stable, user-facing error categories. GitHub response bodies,
// network URLs, runtime configuration, and credentials never reach the page.
func mapWatchError(err error) error {
	var apiError *github.APIError
	switch {
	case errors.Is(err, watch.ErrInvalidRepository):
		return web.ErrWatchInvalid
	case errors.Is(err, watch.ErrPrivateRepository):
		return web.ErrWatchPrivate
	case errors.Is(err, corestore.ErrRepositoryIdentityMismatch):
		return web.ErrWatchIdentity
	case errors.Is(err, corestore.ErrInvalid):
		return web.ErrWatchTopic
	case errors.As(err, &apiError):
		switch apiError.Code {
		case github.CodeNotFoundOrPrivate:
			return web.ErrWatchPrivate
		case github.CodePrimaryRateLimit, github.CodeSecondaryRateLimit:
			return web.ErrWatchRateLimited
		case github.CodeBadRequest, github.CodeValidation:
			return web.ErrWatchInvalid
		}
	}
	return web.ErrWatchUnavailable
}

package app

import (
	"context"
	"errors"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/classification"
	"github.com/xz1220/repotempo/internal/service/watch"
	"github.com/xz1220/repotempo/internal/source/github"
	corestore "github.com/xz1220/repotempo/internal/store"
	"github.com/xz1220/repotempo/internal/web"
)

var _ web.Watcher = (*Runtime)(nil)

func (runtime *Runtime) WatchState(ctx context.Context, repositoryName string) (web.WatchRequest, error) {
	principal, scoped := domain.PrincipalFromContext(ctx)
	if !scoped || principal.UserID <= 0 {
		return web.WatchRequest{}, web.ErrWatchAdminRequired
	}
	name, err := watch.NormalizeRepository(repositoryName)
	if err != nil {
		return web.WatchRequest{}, web.ErrWatchInvalid
	}
	repository, err := runtime.store.GetRepositoryByFullName(ctx, name)
	if errors.Is(err, corestore.ErrNotFound) {
		return web.WatchRequest{}, web.ErrWatchAdminRequired
	}
	if err != nil {
		return web.WatchRequest{}, web.ErrWatchUnavailable
	}
	states, err := runtime.store.UserRepositoryStates(ctx, principal.UserID, []int64{repository.GitHubRepoID})
	if err != nil {
		return web.WatchRequest{}, web.ErrWatchUnavailable
	}
	state := states[repository.GitHubRepoID]
	return web.WatchRequest{Repository: repository.FullName, EditRepository: repository.FullName, Focus: state.IsFocus, Note: state.Note}, nil
}

func (runtime *Runtime) WatchTopics(ctx context.Context) ([]web.TopicRef, error) {
	if principal, scoped := domain.PrincipalFromContext(ctx); scoped && !principal.Admin {
		return []web.TopicRef{}, nil
	}
	topics, err := runtime.store.ListTopics(ctx, domain.TopicActive)
	if err != nil {
		return nil, err
	}
	return mapTopicFilterRefs(topics), nil
}

func (runtime *Runtime) AddWatch(ctx context.Context, request web.WatchRequest) (web.WatchResult, error) {
	name, normalizeErr := watch.NormalizeRepository(request.Repository)
	if normalizeErr != nil {
		return web.WatchResult{}, web.ErrWatchInvalid
	}
	if principal, scoped := domain.PrincipalFromContext(ctx); scoped {
		if principal.UserID <= 0 {
			return web.WatchResult{}, web.ErrWatchAdminRequired
		}
		if !principal.Admin && request.TopicSlug != "" {
			return web.WatchResult{}, web.ErrWatchAdminRequired
		}
		repository, err := runtime.store.GetRepositoryByFullName(ctx, name)
		if errors.Is(err, corestore.ErrNotFound) {
			return web.WatchResult{}, web.ErrWatchAdminRequired
		}
		if err != nil {
			return web.WatchResult{}, web.ErrWatchUnavailable
		}
		if request.EditRepository != "" {
			target, targetErr := watch.NormalizeRepository(request.EditRepository)
			if targetErr != nil || !strings.EqualFold(target, name) {
				return web.WatchResult{}, web.ErrWatchInvalid
			}
			err = runtime.store.SetUserRepositoryState(ctx, principal.UserID, repository.GitHubRepoID, request.Focus, request.Note)
		} else {
			err = runtime.store.MergeUserRepositoryState(ctx, principal.UserID, repository.GitHubRepoID, request.Focus, request.Note)
		}
		if err != nil {
			return web.WatchResult{}, mapWatchError(err)
		}
		return web.WatchResult{ID: repository.GitHubRepoID, FullName: repository.FullName}, nil
	}
	_, client, _, err := runtime.dependencies()
	if err != nil {
		return web.WatchResult{}, web.ErrWatchUnavailable
	}
	service := watch.Service{Store: runtime.store, Resolver: client, Now: runtime.now}
	result, err := service.ImportTracked(ctx, request.Repository, request.Note, request.TopicSlug, request.Focus)
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
	case errors.Is(err, domain.ErrUserRepositoryLimit):
		return web.ErrWatchLimit
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

package watch

import (
	"context"
	"fmt"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
)

type Resolver interface {
	ResolveRepository(context.Context, string, int64) (source.Repository, error)
}

type Store interface {
	UpsertRepository(context.Context, domain.RepositoryObservation) (domain.Repository, bool, error)
	GetRepositoryByFullName(context.Context, string) (domain.Repository, error)
	SetRepositoryMonitoringStatus(context.Context, int64, domain.MonitoringStatus) error
}

type Service struct {
	Store    Store
	Resolver Resolver
	Now      func() time.Time
}

type AddResult struct {
	Repository domain.Repository `json:"repository"`
	Created    bool              `json:"created"`
	DryRun     bool              `json:"dry_run"`
}

func (service Service) Add(ctx context.Context, fullName, note string, focus, dryRun bool) (AddResult, error) {
	if service.Store == nil || service.Resolver == nil {
		return AddResult{}, fmt.Errorf("watch service requires store and GitHub resolver")
	}
	repository, err := service.Resolver.ResolveRepository(ctx, fullName, 0)
	if err != nil {
		return AddResult{}, fmt.Errorf("resolve GitHub repository %q: %w", fullName, err)
	}
	if repository.ID <= 0 || repository.FullName == "" {
		return AddResult{}, fmt.Errorf("GitHub response omitted repository identity")
	}
	now := service.now().UTC()
	description := repository.Description
	language := repository.Language
	status := domain.GitHubActive
	if repository.Private {
		status = domain.GitHubPrivate
	} else if repository.Archived {
		status = domain.GitHubArchived
	}
	observation := domain.RepositoryObservation{
		GitHubRepoID:     repository.ID,
		GitHubNodeID:     repository.NodeID,
		FullName:         repository.FullName,
		HTMLURL:          repository.HTMLURL,
		Description:      &description,
		PrimaryLanguage:  &language,
		GitHubCreatedAt:  repository.CreatedAt,
		Source:           domain.DiscoverySourceManual,
		Profile:          "manual-watchlist",
		DiscoveredAt:     now,
		MonitoringStatus: domain.MonitoringActive,
		GitHubStatus:     status,
		IsFocus:          &focus,
		ManualNote:       &note,
		LastCheckedAt:    &now,
	}
	if dryRun {
		return AddResult{Repository: domain.Repository{
			GitHubRepoID:     repository.ID,
			GitHubNodeID:     repository.NodeID,
			FullName:         repository.FullName,
			HTMLURL:          repository.HTMLURL,
			Description:      repository.Description,
			PrimaryLanguage:  repository.Language,
			MonitoringStatus: domain.MonitoringActive,
			GitHubStatus:     status,
			IsFocus:          focus,
			ManualNote:       note,
		}, Created: true, DryRun: true}, nil
	}
	stored, created, err := service.Store.UpsertRepository(ctx, observation)
	if err != nil {
		return AddResult{}, err
	}
	return AddResult{Repository: stored, Created: created}, nil
}

func (service Service) SetMonitoring(ctx context.Context, fullName string, status domain.MonitoringStatus, dryRun bool) (domain.Repository, error) {
	if service.Store == nil {
		return domain.Repository{}, fmt.Errorf("watch service requires a store")
	}
	if status != domain.MonitoringActive && status != domain.MonitoringPaused {
		return domain.Repository{}, fmt.Errorf("watch status must be active or paused")
	}
	repository, err := service.Store.GetRepositoryByFullName(ctx, fullName)
	if err != nil {
		return domain.Repository{}, fmt.Errorf("resolve repository %q: %w", fullName, err)
	}
	if dryRun {
		repository.MonitoringStatus = status
		return repository, nil
	}
	if err := service.Store.SetRepositoryMonitoringStatus(ctx, repository.GitHubRepoID, status); err != nil {
		return domain.Repository{}, err
	}
	repository.MonitoringStatus = status
	return repository, nil
}

func (service Service) now() time.Time {
	if service.Now != nil {
		return service.Now()
	}
	return time.Now()
}

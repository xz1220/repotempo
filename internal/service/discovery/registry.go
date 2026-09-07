package discovery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
)

type RepositoryStore interface {
	UpsertRepository(context.Context, domain.RepositoryObservation) (domain.Repository, bool, error)
}

type Registry struct {
	Store RepositoryStore
	Now   func() time.Time
}

type RegistryFailure struct {
	RepositoryID int64  `json:"repository_id"`
	FullName     string `json:"full_name"`
	Error        string `json:"error"`
}

type RegistryReport struct {
	CandidateCount int                 `json:"candidate_count"`
	CreatedCount   int                 `json:"created_count"`
	UpdatedCount   int                 `json:"updated_count"`
	SkippedCount   int                 `json:"skipped_count"`
	FailureCount   int                 `json:"failure_count"`
	Failures       []RegistryFailure   `json:"failures"`
	Repositories   []domain.Repository `json:"repositories"`
}

func (registry Registry) Merge(ctx context.Context, candidates []source.Candidate) (RegistryReport, error) {
	if registry.Store == nil {
		return RegistryReport{}, fmt.Errorf("discovery registry requires a store")
	}
	if registry.Now == nil {
		registry.Now = time.Now
	}
	report := RegistryReport{
		CandidateCount: len(candidates),
		Failures:       []RegistryFailure{},
		Repositories:   []domain.Repository{},
	}
	for _, candidate := range source.MergeCandidates(candidates) {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if candidate.Repository.ID <= 0 || candidate.Repository.FullName == "" {
			report.SkippedCount++
			continue
		}
		discoverySource, ok := mapSource(candidate.Source)
		if !ok {
			report.FailureCount++
			report.Failures = append(report.Failures, RegistryFailure{
				RepositoryID: candidate.Repository.ID,
				FullName:     candidate.Repository.FullName,
				Error:        fmt.Sprintf("unsupported discovery source %q", candidate.Source),
			})
			continue
		}
		if candidate.Repository.Fork && discoverySource != domain.DiscoverySourceManual && discoverySource != domain.DiscoverySourceGitHubTrending {
			report.SkippedCount++
			continue
		}
		observedAt := candidate.DiscoveredAt
		if observedAt.IsZero() {
			observedAt = registry.Now().UTC()
		}
		description := candidate.Repository.Description
		language := candidate.Repository.Language
		focus := candidate.IsFocus
		note := candidate.ManualNote
		monitoring := mapMonitoring(candidate.MonitorStatus)
		githubStatus := mapGitHubStatus(candidate.Repository.GitHubStatus)
		switch {
		case candidate.Repository.Private:
			githubStatus = domain.GitHubPrivate
			if candidate.MonitorStatus == "" && discoverySource != domain.DiscoverySourceManual {
				monitoring = domain.MonitoringPaused
			}
		case candidate.Repository.Archived:
			githubStatus = domain.GitHubArchived
			if candidate.MonitorStatus == "" && discoverySource != domain.DiscoverySourceManual {
				monitoring = domain.MonitoringPaused
			}
		case githubStatus == domain.GitHubDeleted:
			if candidate.MonitorStatus == "" {
				monitoring = domain.MonitoringStopped
			}
		case githubStatus == domain.GitHubUnreachable:
			if candidate.MonitorStatus == "" {
				monitoring = domain.MonitoringPaused
			}
		}
		observation := domain.RepositoryObservation{
			GitHubRepoID:     candidate.Repository.ID,
			GitHubNodeID:     candidate.Repository.NodeID,
			FullName:         candidate.Repository.FullName,
			HTMLURL:          candidate.Repository.HTMLURL,
			Description:      &description,
			PrimaryLanguage:  &language,
			GitHubCreatedAt:  candidate.Repository.CreatedAt,
			Source:           discoverySource,
			Profile:          candidate.Profile,
			DiscoveredAt:     observedAt,
			MonitoringStatus: monitoring,
			GitHubStatus:     githubStatus,
			IsFocus:          &focus,
			ManualNote:       &note,
		}
		if etag := candidate.Metadata["etag"]; etag != "" {
			observation.GitHubETag = &etag
		}
		repository, created, err := registry.Store.UpsertRepository(ctx, observation)
		if err != nil {
			report.FailureCount++
			report.Failures = append(report.Failures, RegistryFailure{
				RepositoryID: candidate.Repository.ID,
				FullName:     candidate.Repository.FullName,
				Error:        err.Error(),
			})
			continue
		}
		if created {
			report.CreatedCount++
		} else {
			report.UpdatedCount++
		}
		if err := registry.persistAdditionalSources(ctx, observation, candidate.Metadata["discovery_sources"]); err != nil {
			report.FailureCount++
			report.Failures = append(report.Failures, RegistryFailure{
				RepositoryID: candidate.Repository.ID,
				FullName:     candidate.Repository.FullName,
				Error:        err.Error(),
			})
		}
		report.Repositories = append(report.Repositories, repository)
	}
	return report, nil
}

func (registry Registry) persistAdditionalSources(ctx context.Context, observation domain.RepositoryObservation, sources string) error {
	for _, value := range strings.Split(sources, ",") {
		sourceValue, ok := mapSource(strings.TrimSpace(value))
		if !ok || sourceValue == observation.Source {
			continue
		}
		additional := observation
		additional.Source = sourceValue
		additional.Profile = ""
		if _, _, err := registry.Store.UpsertRepository(ctx, additional); err != nil {
			return fmt.Errorf("persist additional discovery source %s: %w", sourceValue, err)
		}
	}
	return nil
}

func mapSource(value string) (domain.DiscoverySource, bool) {
	switch value {
	case "ossinsight":
		return domain.DiscoverySourceOSSInsight, true
	case "github_search", "github-search":
		return domain.DiscoverySourceGitHubSearch, true
	case "github_trending", "github-trending":
		return domain.DiscoverySourceGitHubTrending, true
	case "legacy":
		return domain.DiscoverySourceLegacy, true
	case "manual":
		return domain.DiscoverySourceManual, true
	default:
		return "", false
	}
}

func mapMonitoring(value string) domain.MonitoringStatus {
	switch value {
	case string(domain.MonitoringPaused):
		return domain.MonitoringPaused
	case string(domain.MonitoringStopped):
		return domain.MonitoringStopped
	default:
		return domain.MonitoringActive
	}
}

func mapGitHubStatus(value string) domain.GitHubStatus {
	switch value {
	case string(domain.GitHubArchived):
		return domain.GitHubArchived
	case string(domain.GitHubDeleted):
		return domain.GitHubDeleted
	case string(domain.GitHubPrivate):
		return domain.GitHubPrivate
	case string(domain.GitHubUnreachable):
		return domain.GitHubUnreachable
	default:
		return domain.GitHubActive
	}
}

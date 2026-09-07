package watch

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/source"
)

var (
	ErrInvalidRepository = errors.New("watch: invalid public GitHub repository")
	ErrPrivateRepository = errors.New("watch: private repositories are not supported")
	ErrMissingStars      = errors.New("watch: GitHub response omitted a valid star count")
)

var repositoryNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9_.-]{1,100}$`)

// NormalizeRepository accepts only GitHub repository URLs and owner/name. The
// result is sent to the configured GitHub API; no user URL is ever fetched.
func NormalizeRepository(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "https://") {
		parsed, err := url.Parse(value)
		if err != nil || (parsed.Host != "github.com" && parsed.Host != "www.github.com") || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
			return "", ErrInvalidRepository
		}
		value = strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), "/")
	}
	value = strings.TrimSuffix(value, ".git")
	if !repositoryNamePattern.MatchString(value) {
		return "", ErrInvalidRepository
	}
	parts := strings.Split(value, "/")
	if strings.HasSuffix(parts[0], "-") || strings.Contains(parts[0], "--") || parts[1] == "." || parts[1] == ".." {
		return "", ErrInvalidRepository
	}
	return value, nil
}

type TrackedStore interface {
	PutWatchedRepository(context.Context, domain.RepositoryObservation, domain.DailySnapshot, string) (domain.Repository, bool, error)
}

type TrackedResult struct {
	AddResult
	GitHubTopics []string
}

// AddTracked saves registry metadata, the first real daily snapshot, and an
// optional manual category in one transaction. Repeated adds preserve prior
// notes, categories, and successful observations.
func (service Service) AddTracked(ctx context.Context, input, note, topicSlug string) (TrackedResult, error) {
	fullName, err := NormalizeRepository(input)
	if err != nil || utf8.RuneCountInString(note) > 2000 || len(topicSlug) > 100 {
		return TrackedResult{}, ErrInvalidRepository
	}
	trackedStore, ok := service.Store.(TrackedStore)
	if !ok || service.Resolver == nil {
		return TrackedResult{}, fmt.Errorf("watch service requires transactional storage and GitHub resolver")
	}
	repository, err := service.Resolver.ResolveRepository(ctx, fullName, 0)
	if err != nil {
		return TrackedResult{}, err
	}
	if err := validatePublicRepository(repository); err != nil {
		return TrackedResult{}, err
	}
	now := service.now().UTC()
	focus := true
	status := domain.GitHubActive
	if repository.Archived {
		status = domain.GitHubArchived
	}
	note = strings.TrimSpace(note)
	observation := domain.RepositoryObservation{
		GitHubRepoID: repository.ID, GitHubNodeID: repository.NodeID,
		FullName: repository.FullName, HTMLURL: "https://github.com/" + repository.FullName,
		Description: &repository.Description, PrimaryLanguage: &repository.Language,
		GitHubCreatedAt: repository.CreatedAt, Source: domain.DiscoverySourceManual,
		Profile: "manual-watchlist", DiscoveredAt: now,
		MonitoringStatus: domain.MonitoringActive, GitHubStatus: status,
		IsFocus: &focus, LastCheckedAt: &now,
	}
	if note != "" {
		observation.ManualNote = &note
	}
	statusCode := 200
	snapshot := domain.DailySnapshot{
		RepositoryID: repository.ID, SnapshotDate: domain.ShanghaiDate(now),
		CapturedAt: now, StarCount: repository.AbsoluteStars,
		FetchStatus: domain.FetchSuccess, HTTPStatus: &statusCode,
	}
	stored, created, err := trackedStore.PutWatchedRepository(ctx, observation, snapshot, strings.TrimSpace(topicSlug))
	if err != nil {
		return TrackedResult{}, err
	}
	return TrackedResult{AddResult: AddResult{Repository: stored, Created: created}, GitHubTopics: repository.Topics}, nil
}

func validatePublicRepository(repository source.Repository) error {
	if repository.Private {
		return ErrPrivateRepository
	}
	canonical, err := NormalizeRepository(repository.FullName)
	if repository.ID <= 0 || err != nil || canonical != repository.FullName {
		return ErrInvalidRepository
	}
	if repository.AbsoluteStars == nil || *repository.AbsoluteStars < 0 {
		return ErrMissingStars
	}
	return nil
}

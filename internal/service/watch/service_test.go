package watch

import (
	"context"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/source"
)

type fakeResolver struct{}

func (fakeResolver) ResolveRepository(context.Context, string, int64) (source.Repository, error) {
	stars := int64(12)
	return source.Repository{ID: 99, FullName: "owner/repo", HTMLURL: "https://github.com/owner/repo", AbsoluteStars: &stars}, nil
}

type fakeWatchStore struct {
	upserts int
	status  domain.MonitoringStatus
}

func (store *fakeWatchStore) UpsertRepository(_ context.Context, observation domain.RepositoryObservation) (domain.Repository, bool, error) {
	store.upserts++
	return domain.Repository{GitHubRepoID: observation.GitHubRepoID, FullName: observation.FullName, MonitoringStatus: observation.MonitoringStatus}, true, nil
}

func (store *fakeWatchStore) GetRepositoryByFullName(context.Context, string) (domain.Repository, error) {
	return domain.Repository{GitHubRepoID: 99, FullName: "owner/repo", MonitoringStatus: domain.MonitoringActive}, nil
}

func (store *fakeWatchStore) SetRepositoryMonitoringStatus(_ context.Context, _ int64, status domain.MonitoringStatus) error {
	store.status = status
	return nil
}

func TestAddDryRunStillResolvesButDoesNotWrite(t *testing.T) {
	store := &fakeWatchStore{}
	service := Service{Store: store, Resolver: fakeResolver{}, Now: func() time.Time { return time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC) }}
	result, err := service.Add(context.Background(), "owner/repo", "benchmark", true, true)
	if err != nil {
		t.Fatal(err)
	}
	if !result.DryRun || result.Repository.GitHubRepoID != 99 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if store.upserts != 0 {
		t.Fatal("dry run wrote repository")
	}
}

func TestPause(t *testing.T) {
	store := &fakeWatchStore{}
	service := Service{Store: store}
	repository, err := service.SetMonitoring(context.Background(), "owner/repo", domain.MonitoringPaused, false)
	if err != nil {
		t.Fatal(err)
	}
	if store.status != domain.MonitoringPaused || repository.MonitoringStatus != domain.MonitoringPaused {
		t.Fatal("repository was not paused")
	}
}

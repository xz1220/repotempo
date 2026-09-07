package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
)

type fakeRepositoryStore struct {
	observations []domain.RepositoryObservation
	errorForID   int64
}

func (store *fakeRepositoryStore) UpsertRepository(_ context.Context, observation domain.RepositoryObservation) (domain.Repository, bool, error) {
	if observation.GitHubRepoID == store.errorForID {
		return domain.Repository{}, false, errors.New("write failed")
	}
	store.observations = append(store.observations, observation)
	return domain.Repository{GitHubRepoID: observation.GitHubRepoID, FullName: observation.FullName}, true, nil
}

func TestRegistryDeduplicatesByRepositoryIDAndSkipsDiscoveredForks(t *testing.T) {
	store := &fakeRepositoryStore{}
	now := time.Date(2026, 8, 30, 2, 0, 0, 0, time.UTC)
	registry := Registry{Store: store, Now: func() time.Time { return now }}
	candidates := []source.Candidate{
		{Repository: source.Repository{ID: 1, FullName: "owner/old"}, Source: "ossinsight"},
		{Repository: source.Repository{ID: 1, FullName: "owner/new"}, Source: "github_search"},
		{Repository: source.Repository{ID: 2, FullName: "owner/fork", Fork: true}, Source: "github_search"},
	}
	report, err := registry.Merge(context.Background(), candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.observations) != 2 || store.observations[0].FullName != "owner/new" {
		t.Fatalf("unexpected observations: %#v", store.observations)
	}
	if store.observations[0].Source == store.observations[1].Source {
		t.Fatalf("additional discovery source was not persisted: %#v", store.observations)
	}
	if report.CreatedCount != 1 || report.SkippedCount != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRegistryPreservesLegacyGitHubStatus(t *testing.T) {
	store := &fakeRepositoryStore{}
	registry := Registry{Store: store}
	_, err := registry.Merge(context.Background(), []source.Candidate{{
		Repository: source.Repository{ID: 1, FullName: "owner/deleted", GitHubStatus: "deleted"},
		Source:     "legacy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.observations[0].GitHubStatus; got != domain.GitHubDeleted {
		t.Fatalf("GitHub status = %s", got)
	}
	if got := store.observations[0].MonitoringStatus; got != domain.MonitoringStopped {
		t.Fatalf("deleted repository monitoring status = %s", got)
	}
}

func TestRegistryAllowsManualForkAndPausesArchivedDiscovery(t *testing.T) {
	store := &fakeRepositoryStore{}
	registry := Registry{Store: store}
	_, err := registry.Merge(context.Background(), []source.Candidate{
		{Repository: source.Repository{ID: 1, FullName: "owner/manual-fork", Fork: true}, Source: "manual"},
		{Repository: source.Repository{ID: 2, FullName: "owner/archive", Archived: true}, Source: "ossinsight"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := store.observations[0].MonitoringStatus; got != domain.MonitoringActive {
		t.Fatalf("manual fork status = %s", got)
	}
	if got := store.observations[1].MonitoringStatus; got != domain.MonitoringPaused {
		t.Fatalf("archived status = %s", got)
	}
}

package snapshot

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/source"
	"github.com/xz1220/github-radar/internal/source/github"
	corestore "github.com/xz1220/github-radar/internal/store"
)

type fetcherFunc func(context.Context, int64, string) (github.RepositoryResult, error)

func (function fetcherFunc) FetchRepositoryByID(ctx context.Context, id int64, etag string) (github.RepositoryResult, error) {
	return function(ctx, id, etag)
}

type fakeSnapshotStore struct {
	repositories []domain.Repository
	today        map[int64]domain.DailySnapshot
	latest       map[int64]domain.DailySnapshot
	writes       []domain.DailySnapshot
	upserts      []domain.RepositoryObservation
	monitoring   map[int64]domain.MonitoringStatus
	githubStatus map[int64]domain.GitHubStatus
}

func newFakeSnapshotStore(repository domain.Repository) *fakeSnapshotStore {
	return &fakeSnapshotStore{
		repositories: []domain.Repository{repository},
		today:        map[int64]domain.DailySnapshot{},
		latest:       map[int64]domain.DailySnapshot{},
		monitoring:   map[int64]domain.MonitoringStatus{},
		githubStatus: map[int64]domain.GitHubStatus{},
	}
}

func (store *fakeSnapshotStore) ListRepositories(context.Context, domain.RepositoryFilter) ([]domain.Repository, error) {
	return store.repositories, nil
}

func (store *fakeSnapshotStore) GetDailySnapshot(_ context.Context, id int64, _ domain.Date) (domain.DailySnapshot, error) {
	if snapshot, ok := store.today[id]; ok {
		return snapshot, nil
	}
	return domain.DailySnapshot{}, corestore.ErrNotFound
}

func (store *fakeSnapshotStore) GetLatestSuccessfulSnapshot(_ context.Context, id int64, _ domain.Date) (domain.DailySnapshot, error) {
	if snapshot, ok := store.latest[id]; ok {
		return snapshot, nil
	}
	return domain.DailySnapshot{}, corestore.ErrNotFound
}

func (store *fakeSnapshotStore) PutDailySnapshot(_ context.Context, snapshot domain.DailySnapshot) (domain.SnapshotWriteResult, error) {
	store.writes = append(store.writes, snapshot)
	store.today[snapshot.RepositoryID] = snapshot
	return domain.SnapshotWriteResult{Disposition: domain.SnapshotInserted, Snapshot: snapshot}, nil
}

func (store *fakeSnapshotStore) UpsertRepository(_ context.Context, observation domain.RepositoryObservation) (domain.Repository, bool, error) {
	store.upserts = append(store.upserts, observation)
	return domain.Repository{GitHubRepoID: observation.GitHubRepoID, FullName: observation.FullName}, false, nil
}

func (store *fakeSnapshotStore) SetRepositoryMonitoringStatus(_ context.Context, id int64, status domain.MonitoringStatus) error {
	store.monitoring[id] = status
	return nil
}

func (store *fakeSnapshotStore) SetRepositoryGitHubStatus(_ context.Context, id int64, status domain.GitHubStatus) error {
	store.githubStatus[id] = status
	return nil
}

func testRepository() domain.Repository {
	return domain.Repository{
		GitHubRepoID:     42,
		FullName:         "owner/repo",
		FirstSeenSource:  domain.DiscoverySourceManual,
		FirstSeenAt:      time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		LastDiscoveredAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		MonitoringStatus: domain.MonitoringActive,
		GitHubStatus:     domain.GitHubActive,
		GitHubETag:       `"old"`,
	}
}

func TestRunWritesSuccessfulAbsoluteSnapshot(t *testing.T) {
	repository := testRepository()
	store := newFakeSnapshotStore(repository)
	stars := int64(1234)
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 200, ETag: `"new"`, Repository: source.Repository{ID: 42, FullName: "owner/repo", AbsoluteStars: &stars}}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.SuccessCount != 1 || report.FailureCount != 0 || len(store.writes) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if store.writes[0].StarCount == nil || *store.writes[0].StarCount != stars {
		t.Fatalf("unexpected snapshot: %#v", store.writes[0])
	}
}

func TestRunRecordsUpstreamFailureWithNullStar(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 503}, &github.APIError{Code: github.CodeUpstream, StatusCode: 503}
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.FailureCount != 1 || len(store.writes) != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if store.writes[0].StarCount != nil || store.writes[0].ErrorCode != string(github.CodeUpstream) {
		t.Fatalf("failure snapshot was not explicit: %#v", store.writes[0])
	}
}

func TestRunUsesSuccessfulBaselineForNotModified(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	stars := int64(900)
	store.latest[42] = domain.DailySnapshot{RepositoryID: 42, FetchStatus: domain.FetchSuccess, StarCount: &stars}
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 304, NotModified: true, ETag: `"old"`}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	if _, err := service.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if store.writes[0].StarCount == nil || *store.writes[0].StarCount != stars || *store.writes[0].HTTPStatus != 304 {
		t.Fatalf("unexpected 304 snapshot: %#v", store.writes[0])
	}
}

func TestRunSkipsExistingSuccessWithoutAPICall(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	stars := int64(100)
	store.today[42] = domain.DailySnapshot{RepositoryID: 42, FetchStatus: domain.FetchSuccess, StarCount: &stars}
	called := false
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			called = true
			return github.RepositoryResult{}, errors.New("should not run")
		}),
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if called || report.SuccessCount != 1 || report.SkippedCount != 1 {
		t.Fatalf("existing success was not skipped: %#v", report)
	}
}

func TestRunStopsRepositoryOnIdentityMismatch(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 200}, &github.RepositoryIDMismatchError{Expected: 42, Actual: 77, FullName: "owner/repo"}
		}),
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.FailureCount != 1 || store.monitoring[42] != domain.MonitoringStopped {
		t.Fatalf("identity mismatch did not stop monitoring: %#v", report)
	}
}

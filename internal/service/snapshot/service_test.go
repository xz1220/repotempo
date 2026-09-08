package snapshot

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
	"github.com/xz1220/repotempo/internal/source/github"
	corestore "github.com/xz1220/repotempo/internal/store"
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
	putErrors    map[int64]error
	getErrors    map[int64]error
	latestErrors map[int64]error
	upsertError  error
}

func newFakeSnapshotStore(repository domain.Repository) *fakeSnapshotStore {
	return &fakeSnapshotStore{
		repositories: []domain.Repository{repository},
		today:        map[int64]domain.DailySnapshot{},
		latest:       map[int64]domain.DailySnapshot{},
		monitoring:   map[int64]domain.MonitoringStatus{},
		githubStatus: map[int64]domain.GitHubStatus{},
		putErrors:    map[int64]error{},
		getErrors:    map[int64]error{},
		latestErrors: map[int64]error{},
	}
}

func (store *fakeSnapshotStore) ListRepositories(context.Context, domain.RepositoryFilter) ([]domain.Repository, error) {
	return store.repositories, nil
}

func (store *fakeSnapshotStore) GetDailySnapshot(_ context.Context, id int64, _ domain.Date) (domain.DailySnapshot, error) {
	if err := store.getErrors[id]; err != nil {
		return domain.DailySnapshot{}, err
	}
	if snapshot, ok := store.today[id]; ok {
		return snapshot, nil
	}
	return domain.DailySnapshot{}, corestore.ErrNotFound
}

func (store *fakeSnapshotStore) GetLatestSuccessfulSnapshot(_ context.Context, id int64, _ domain.Date) (domain.DailySnapshot, error) {
	if err := store.latestErrors[id]; err != nil {
		return domain.DailySnapshot{}, err
	}
	if snapshot, ok := store.latest[id]; ok {
		return snapshot, nil
	}
	return domain.DailySnapshot{}, corestore.ErrNotFound
}

func (store *fakeSnapshotStore) PutDailySnapshot(_ context.Context, snapshot domain.DailySnapshot) (domain.SnapshotWriteResult, error) {
	if err := store.putErrors[snapshot.RepositoryID]; err != nil {
		return domain.SnapshotWriteResult{}, err
	}
	store.writes = append(store.writes, snapshot)
	store.today[snapshot.RepositoryID] = snapshot
	return domain.SnapshotWriteResult{Disposition: domain.SnapshotInserted, Snapshot: snapshot}, nil
}

func (store *fakeSnapshotStore) UpsertRepository(_ context.Context, observation domain.RepositoryObservation) (domain.Repository, bool, error) {
	if store.upsertError != nil {
		return domain.Repository{}, false, store.upsertError
	}
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
		GitHubTopics:     []string{},
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

func TestRunRecordsFailureWhenNotModifiedHasNoBaseline(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 304, NotModified: true, ETag: `"old"`}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.FailureCount != 1 || store.writes[0].ErrorCode != "not_modified_without_baseline" || store.writes[0].StarCount != nil {
		t.Fatalf("unexpected baseline failure: report=%#v snapshot=%#v", report, store.writes[0])
	}
}

func TestRunDoesNotSendImportedETagWithoutStarBaseline(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	stars := int64(123)
	var receivedETag string
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(_ context.Context, _ int64, etag string) (github.RepositoryResult, error) {
			receivedETag = etag
			return github.RepositoryResult{HTTPStatus: 200, Repository: source.Repository{ID: 42, FullName: "owner/repo", AbsoluteStars: &stars}}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	if _, err := service.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if receivedETag != "" {
		t.Fatalf("sent ETag %q without a successful star baseline", receivedETag)
	}
}

func TestRunRepairsSameDayFailureOnSuccessfulRetry(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	attempt := 0
	stars := int64(321)
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			attempt++
			if attempt == 1 {
				return github.RepositoryResult{HTTPStatus: 503}, &github.APIError{Code: github.CodeUpstream, StatusCode: 503}
			}
			return github.RepositoryResult{HTTPStatus: 200, Repository: source.Repository{ID: 42, FullName: "owner/repo", AbsoluteStars: &stars}}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	first, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if first.FailureCount != 1 || second.SuccessCount != 1 || store.today[42].StarCount == nil || *store.today[42].StarCount != stars {
		t.Fatalf("failure was not repaired: first=%#v second=%#v snapshot=%#v", first, second, store.today[42])
	}
}

func TestRunNeverUsesOSSWindowStarsAsAbsoluteStars(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	absolute := int64(1000)
	window := int64(77)
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 200, Repository: source.Repository{ID: 42, FullName: "owner/repo", AbsoluteStars: &absolute}}, nil
		}),
		Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) },
	}
	if _, err := service.Run(context.Background(), map[int64]OSSEvidence{42: {WindowStars: &window}}); err != nil {
		t.Fatal(err)
	}
	if got := *store.writes[0].StarCount; got != absolute {
		t.Fatalf("absolute stars = %d, expected %d", got, absolute)
	}
	if got := *store.writes[0].OSSWindowStars; got != window {
		t.Fatalf("window stars = %d, expected %d", got, window)
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
	if called || report.SuccessCount != 0 || report.SkippedCount != 1 {
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

func TestRunContinuesAfterOneRepositorySnapshotWriteFails(t *testing.T) {
	first := testRepository()
	first.GitHubRepoID = 1
	first.FullName = "owner/first"
	second := testRepository()
	second.GitHubRepoID = 2
	second.FullName = "owner/second"
	store := newFakeSnapshotStore(first)
	store.repositories = []domain.Repository{first, second}
	store.putErrors[1] = errors.New("disk write failed")
	stars := int64(50)
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(_ context.Context, id int64, _ string) (github.RepositoryResult, error) {
			name := "owner/first"
			if id == 2 {
				name = "owner/second"
			}
			return github.RepositoryResult{HTTPStatus: 200, ETag: `"new"`, Repository: source.Repository{ID: id, FullName: name, AbsoluteStars: &stars}}, nil
		}),
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.TargetCount != 2 || report.SuccessCount != 1 || report.FailureCount != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(store.upserts) != 1 || store.upserts[0].GitHubRepoID != 2 {
		t.Fatalf("ETag updates = %#v; failed write must not advance repository 1", store.upserts)
	}
}

func TestRunKeepsSuccessfulSnapshotWhenMetadataUpdateFails(t *testing.T) {
	store := newFakeSnapshotStore(testRepository())
	store.upsertError = errors.New("metadata unavailable")
	stars := int64(77)
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			return github.RepositoryResult{HTTPStatus: 200, ETag: `"new"`, Repository: source.Repository{ID: 42, FullName: "owner/repo", AbsoluteStars: &stars}}, nil
		}),
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.SuccessCount != 1 || report.FailureCount != 0 || report.MetadataFailureCount != 1 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if snapshotValue := store.today[42]; snapshotValue.StarCount == nil || *snapshotValue.StarCount != stars {
		t.Fatalf("successful star snapshot was lost: %#v", snapshotValue)
	}
}

func TestRunContinuesAfterSnapshotLookupAndBaselineErrors(t *testing.T) {
	first := testRepository()
	first.GitHubRepoID = 1
	first.FullName = "owner/lookup"
	second := testRepository()
	second.GitHubRepoID = 2
	second.FullName = "owner/baseline"
	store := newFakeSnapshotStore(first)
	store.repositories = []domain.Repository{first, second}
	store.getErrors[1] = errors.New("lookup unavailable")
	store.latestErrors[2] = errors.New("baseline unavailable")
	called := false
	service := Service{
		Store: store,
		GitHub: fetcherFunc(func(context.Context, int64, string) (github.RepositoryResult, error) {
			called = true
			return github.RepositoryResult{}, nil
		}),
	}
	report, err := service.Run(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if called || report.TargetCount != 2 || report.FailureCount != 2 || len(report.Failures) != 2 {
		t.Fatalf("unexpected degraded report: %#v", report)
	}
}

func TestRunStopsAfterGitHubAPIAccessFailureWithoutRequestingOtherRepositories(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			first := testRepository()
			second := testRepository()
			second.GitHubRepoID, second.FullName = 43, "owner/second"
			store := newFakeSnapshotStore(first)
			store.repositories = []domain.Repository{first, second}
			calls := 0
			upstream := &github.APIError{StatusCode: status, Code: github.CodeForbidden}
			service := Service{Store: store, GitHub: fetcherFunc(func(_ context.Context, id int64, _ string) (github.RepositoryResult, error) {
				calls++
				if id != first.GitHubRepoID {
					t.Errorf("requested another repository after access failed: %d", id)
				}
				return github.RepositoryResult{HTTPStatus: status}, fmt.Errorf("fetch repository: %w", upstream)
			})}
			report, err := service.Run(context.Background(), nil)
			if !github.IsAccessBlocked(err) || !errors.Is(err, upstream) {
				t.Fatalf("blocking API error was not returned: %v", err)
			}
			if calls != 1 || report.TargetCount != 2 || report.FailureCount != 1 || report.SuccessCount != 0 || report.SkippedCount != 0 || len(report.Failures) != 1 {
				t.Fatalf("unexpected aborted report: calls=%d report=%#v", calls, report)
			}
			if len(store.writes) != 1 || store.writes[0].RepositoryID != first.GitHubRepoID || store.writes[0].FetchStatus != domain.FetchFailed || store.writes[0].StarCount != nil || store.writes[0].HTTPStatus == nil || *store.writes[0].HTTPStatus != status {
				t.Fatalf("received failure was not preserved accurately: %#v", store.writes)
			}
			if _, exists := store.today[second.GitHubRepoID]; exists {
				t.Fatal("a snapshot was fabricated for an unrequested repository")
			}
			if len(store.monitoring) != 0 || len(store.githubStatus) != 0 {
				t.Fatal("global API access failure changed repository lifecycle state")
			}
		})
	}
}

func TestRunContinuesAfterNotFoundAndOrdinaryRepositoryFailures(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		err    error
	}{
		{"not found", 404, &github.APIError{StatusCode: 404, Code: github.CodeNotFoundOrPrivate}},
		{"upstream", 503, &github.APIError{StatusCode: 503, Code: github.CodeUpstream}},
		{"ordinary error", 0, errors.New("one repository failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			first, second := testRepository(), testRepository()
			second.GitHubRepoID, second.FullName = 43, "owner/second"
			store := newFakeSnapshotStore(first)
			store.repositories = []domain.Repository{first, second}
			calls, stars := 0, int64(100)
			service := Service{Store: store, GitHub: fetcherFunc(func(_ context.Context, id int64, _ string) (github.RepositoryResult, error) {
				calls++
				if id == first.GitHubRepoID {
					return github.RepositoryResult{HTTPStatus: test.status}, test.err
				}
				return github.RepositoryResult{HTTPStatus: 200, Repository: source.Repository{ID: second.GitHubRepoID, FullName: second.FullName, AbsoluteStars: &stars}}, nil
			})}
			report, err := service.Run(context.Background(), nil)
			if err != nil || calls != 2 || report.FailureCount != 1 || report.SuccessCount != 1 {
				t.Fatalf("ordinary failure aborted batch: calls=%d report=%#v error=%v", calls, report, err)
			}
			if store.today[first.GitHubRepoID].StarCount != nil || store.today[second.GitHubRepoID].StarCount == nil || *store.today[second.GitHubRepoID].StarCount != 100 {
				t.Fatal("actual failure and later successful snapshot were not both preserved")
			}
		})
	}
}

package snapshot

import (
	"context"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
	"github.com/xz1220/repotempo/internal/source/github"
	"testing"
	"time"
)

func TestUnknownTopicsForceFullResponseAnd304NeverClearsTopics(t *testing.T) {
	repository := testRepository()
	repository.GitHubTopics = nil
	repository.FirstSeenSource = domain.DiscoverySourceLegacy
	store := newFakeSnapshotStore(repository)
	stars := int64(100)
	store.latest[42] = domain.DailySnapshot{RepositoryID: 42, FetchStatus: domain.FetchSuccess, StarCount: &stars}
	receivedETag := "not-called"
	service := Service{Store: store, Now: func() time.Time { return time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC) }, GitHub: fetcherFunc(func(_ context.Context, _ int64, etag string) (github.RepositoryResult, error) {
		receivedETag = etag
		return github.RepositoryResult{HTTPStatus: 200, ETag: "fresh", Repository: source.Repository{ID: 42, FullName: "owner/repo", Topics: []string{}, AbsoluteStars: &stars}}, nil
	})}
	if _, err := service.Run(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if receivedETag != "" || len(store.upserts) != 1 || store.upserts[0].GitHubTopics == nil || *store.upserts[0].GitHubTopics == nil {
		t.Fatalf("bootstrap etag=%q observations=%v", receivedETag, store.upserts)
	}
	repository.GitHubTopics = []string{}
	etag, err := service.safeETag(context.Background(), repository, domain.Date("2026-08-30"))
	if err != nil || etag != repository.GitHubETag {
		t.Fatalf("known empty topics should permit conditional fetch: %q %v", etag, err)
	}
	store.upserts = nil
	if err := service.updateRepository(context.Background(), repository, github.RepositoryResult{HTTPStatus: 304, NotModified: true, ETag: "fresh", Repository: source.Repository{Topics: []string{}}}, service.Now()); err != nil {
		t.Fatal(err)
	}
	if len(store.upserts) != 1 || store.upserts[0].GitHubTopics != nil || store.upserts[0].ResearchTags != nil {
		t.Fatal("304 observation attempted to clear metadata tags")
	}
}

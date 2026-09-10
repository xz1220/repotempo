package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func TestRepositoryTrendOffsetPaginationAndNameSort(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	names := []string{"team/zulu", "team/Alpha", "team/echo", "team/bravo", "team/delta"}
	for index, name := range names {
		focus := index%2 == 0
		if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: int64(index + 1),
			FullName:     name,
			Source:       domain.DiscoverySourceManual,
			DiscoveredAt: testNow.Add(-time.Hour),
			IsFocus:      &focus,
		}); err != nil {
			t.Fatal(err)
		}
	}
	futureFocus := true
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 99,
		FullName:     "team/future",
		Source:       domain.DiscoverySourceManual,
		DiscoveredAt: testNow.AddDate(0, 0, 1),
		IsFocus:      &futureFocus,
	}); err != nil {
		t.Fatal(err)
	}

	page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{
		AsOf:       date("2026-08-30"),
		WindowDays: 7,
		Sort:       domain.RepositoryTrendSortName,
		Limit:      2,
		Offset:     2,
	})
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int64, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.Repository.GitHubRepoID)
	}
	if !reflect.DeepEqual(ids, []int64{5, 3}) {
		t.Fatalf("name-sorted offset page IDs = %v, want [5 3]", ids)
	}
	if page.Total != 5 || page.RegistryTotal != 5 || page.FocusTotal != 3 || !page.HasMore {
		t.Fatalf("page metadata = %+v", page)
	}
	outOfRange, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{
		AsOf: date("2026-08-30"), WindowDays: 7, Sort: domain.RepositoryTrendSortName, Limit: 2, Offset: int(^uint(0) >> 1),
	})
	if err != nil || len(outOfRange.Items) != 0 || outOfRange.Total != 5 {
		t.Fatalf("out-of-range offset page = (%+v, %v)", outOfRange, err)
	}

	filtered, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{
		AsOf: date("2026-08-30"), Search: "bravo", Sort: domain.RepositoryTrendSortName, Limit: 6,
	})
	if err != nil || filtered.Total != 1 || filtered.RegistryTotal != 5 || filtered.FocusTotal != 3 {
		t.Fatalf("global counts changed with a page filter: page=%+v err=%v", filtered, err)
	}
}

func TestRepositoryTrendRejectsInvalidOffsetAndNameCursor(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), Offset: -1}
	if _, err := store.ListRepositoryTrends(ctx, query); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("negative offset error = %v", err)
	}
	cursor := int64(1)
	query = domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), Sort: domain.RepositoryTrendSortName, AfterID: &cursor}
	if _, err := store.ListRepositoryTrends(ctx, query); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("name cursor error = %v", err)
	}
}

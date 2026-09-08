package sqlite

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func TestOnlyExplicitManualChoicesCanSetRepositoryFocus(t *testing.T) {
	ctx := context.Background()
	for _, fixture := range []struct {
		source  domain.DiscoverySource
		profile string
	}{
		{domain.DiscoverySourceLegacy, ""},
		{domain.DiscoverySourceOSSInsight, ""},
		{domain.DiscoverySourceGitHubSearch, "popular"},
		{domain.DiscoverySourceGitHubTrending, "daily"},
		{domain.DiscoverySourceManual, "config-watchlist"},
	} {
		t.Run(string(fixture.source)+"/"+fixture.profile, func(t *testing.T) {
			store, _ := newTestStore(t)
			observation := domain.RepositoryObservation{
				GitHubRepoID: 1, FullName: "owner/project", Source: fixture.source, Profile: fixture.profile,
				DiscoveredAt: testNow, IsFocus: pointer(true),
			}
			stored, created, err := store.UpsertRepository(ctx, observation)
			if err != nil || !created || stored.IsFocus {
				t.Fatalf("new imported/discovered repo became focus: %+v %v", stored, err)
			}
			for _, focus := range []bool{true, false} {
				observation.IsFocus = &focus
				stored, _, err = store.UpsertRepository(ctx, observation)
				if err != nil || stored.IsFocus {
					t.Fatal("rediscovery/import raised personal focus")
				}
			}
			if err := store.SetRepositoryFocus(ctx, 1, true); err != nil {
				t.Fatal(err)
			}
			for _, focus := range []bool{false, true} {
				observation.IsFocus = &focus
				stored, _, err = store.UpsertRepository(ctx, observation)
				if err != nil || !stored.IsFocus {
					t.Fatal("rediscovery/import cleared an explicit focus choice")
				}
			}
			if err := store.SetRepositoryFocus(ctx, 1, false); err != nil {
				t.Fatal(err)
			}
			observation.IsFocus = pointer(true)
			stored, _, err = store.UpsertRepository(ctx, observation)
			if err != nil || stored.IsFocus {
				t.Fatal("replayed import resurrected a cancelled personal focus")
			}
		})
	}
	store, _ := newTestStore(t)
	for id, profile := range []string{"manual-watchlist", "manual-import", ""} {
		stored, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: int64(id + 1), FullName: fmt.Sprintf("owner/manual-%d", id), Source: domain.DiscoverySourceManual,
			Profile: profile, IsFocus: pointer(true),
		})
		if err != nil || !stored.IsFocus {
			t.Fatalf("explicit manual action not applied: %+v %v", stored, err)
		}
	}
}

func TestSetRepositoryFocusDoesNotChangeEvidenceOrMonitoring(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	before := addRepository(t, store, 1, "owner/project")
	putSuccess(t, store, 1, "2026-08-30", 123)
	analysis, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "保留项目说明", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return testNow.Add(time.Hour) }
	for _, value := range []bool{true, false, false} {
		if err := store.SetRepositoryFocus(ctx, 1, value); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetRepository(ctx, 1)
		if err != nil || stored.IsFocus != value {
			t.Fatalf("focus did not change: %+v %v", stored, err)
		}
		stored.IsFocus, stored.UpdatedAt = before.IsFocus, before.UpdatedAt
		if !reflect.DeepEqual(stored, before) {
			t.Fatal("focus action changed unrelated repository state")
		}
	}
	current, err := store.getRepositoryAnalysis(ctx, 1)
	if err != nil || !reflect.DeepEqual(current, &analysis) {
		t.Fatal("focus action changed AI reading")
	}
	snapshot, err := store.GetDailySnapshot(ctx, 1, date("2026-08-30"))
	if err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 123 {
		t.Fatal("focus action changed Star history")
	}
	if err := store.SetRepositoryFocus(ctx, 0, true); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("invalid ID: %v", err)
	}
	if err := store.SetRepositoryFocus(ctx, 999, true); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatalf("missing ID: %v", err)
	}
}

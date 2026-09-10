package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func TestUserWatchlistsHaveSeparateFilteringCountsAndOwnership(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []int64{101, 202} {
		if err := store.PutAuthSession(ctx, authHashFixture(int(id)), authSessionFixture(id, testNow)); err != nil {
			t.Fatal(err)
		}
	}
	addRepository(t, store, 1, "owner/one")
	addRepository(t, store, 2, "owner/two")
	if err := store.SetRepositoryFocus(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	a := domain.WithPrincipal(ctx, domain.Principal{UserID: 101, Login: "first"})
	b := domain.WithPrincipal(ctx, domain.Principal{UserID: 202, Login: "second"})
	anonymous := domain.WithPrincipal(ctx, domain.Principal{})
	if err := store.SetUserRepositoryState(a, 101, 2, true, "Only A can see this"); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserRepositoryState(b, 202, 1, true, "Only B can see this"); err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		ctx    context.Context
		wantID int64
		total  int
	}{{a, 2, 1}, {b, 1, 1}, {anonymous, 0, 0}} {
		page, err := store.ListRepositoryTrends(item.ctx, domain.RepositoryTrendQuery{AsOf: domain.ShanghaiDate(testNow), WindowDays: 7, OnlyFocus: true})
		if err != nil || page.Total != item.total || page.FocusTotal != item.total || len(page.Items) != item.total {
			t.Fatalf("personal page=%+v err=%v", page, err)
		}
		if item.total > 0 && page.Items[0].Repository.GitHubRepoID != item.wantID {
			t.Fatalf("wrong account's repository: %+v", page.Items)
		}
		radar, err := store.RadarOverview(item.ctx, domain.RepositoryTrendQuery{AsOf: domain.ShanghaiDate(testNow), OnlyFocus: true})
		if err != nil || radar.Coverage.ScopeCount != item.total {
			t.Fatalf("radar scope=%+v err=%v", radar.Coverage, err)
		}
	}
	if err := store.SetUserRepositoryFocus(a, 202, 1, false); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("cross-account write accepted: %v", err)
	}
	if _, err := store.UserRepositoryStates(a, 202, []int64{1}); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("cross-account read accepted: %v", err)
	}
	if err := store.SetUserRepositoryFocus(a, 101, 2, false); err != nil {
		t.Fatal(err)
	}
	states, err := store.UserRepositoryStates(a, 101, []int64{2})
	if err != nil || states[2].IsFocus || states[2].Note != "Only A can see this" {
		t.Fatalf("unfollow destroyed note: %+v %v", states, err)
	}
	legacy, err := store.GetRepository(ctx, 1)
	if err != nil || !legacy.IsFocus {
		t.Fatal("personal choices mutated operator legacy workspace")
	}
}

func TestLegacyWorkspaceAdoptionDoesNotChooseFirstPublicUserOrRepeat(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/one")
	if _, err := store.db.Exec(`UPDATE repositories SET is_focus=1, manual_note='private legacy note' WHERE github_repo_id=1`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAuthSession(ctx, authHashFixture(99), authSessionFixture(99, testNow)); err != nil {
		t.Fatal(err)
	}
	if err := store.AdoptLegacyWorkspace(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if err := store.AdoptLegacyWorkspace(ctx, 99); err != nil {
		t.Fatal(err)
	}
	owner, err := store.UserRepositoryStates(ctx, 42, []int64{1})
	if err != nil || !owner[1].IsFocus || owner[1].Note != "private legacy note" {
		t.Fatalf("legacy owner=%+v %v", owner, err)
	}
	public, err := store.UserRepositoryStates(ctx, 99, []int64{1})
	if err != nil || len(public) != 0 {
		t.Fatalf("public user inherited private data: %+v %v", public, err)
	}
	if err := store.SetUserRepositoryFocus(ctx, 42, 1, false); err != nil {
		t.Fatal(err)
	}
	if err := store.AdoptLegacyWorkspace(ctx, 42); err != nil {
		t.Fatal(err)
	}
	owner, err = store.UserRepositoryStates(ctx, 42, []int64{1})
	if err != nil || owner[1].IsFocus {
		t.Fatal("restart resurrected legacy follow")
	}
	legacy, err := store.GetRepository(ctx, 1)
	if err != nil || legacy.ManualNote != "private legacy note" {
		t.Fatal("migration lost legacy data")
	}
}

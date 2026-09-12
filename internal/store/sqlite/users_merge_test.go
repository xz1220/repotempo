package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func TestMergePersonalRepositoryPreservesStateAndExplicitClear(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/one")
	for _, id := range []int64{101, 202} {
		if err := store.PutAuthSession(ctx, authHashFixture(int(id)), authSessionFixture(id, testNow)); err != nil {
			t.Fatal(err)
		}
	}
	a := domain.WithPrincipal(ctx, domain.Principal{UserID: 101})
	if err := store.MergeUserRepositoryState(a, 101, 1, false, "first note"); err != nil {
		t.Fatal(err)
	}
	if err := store.MergeUserRepositoryState(a, 101, 1, true, "replacement"); err != nil {
		t.Fatal(err)
	}
	if err := store.MergeUserRepositoryState(a, 101, 1, false, ""); err != nil {
		t.Fatal(err)
	}
	if err := store.MergeUserRepositoryState(a, 202, 1, true, "cross-account"); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("cross-account=%v", err)
	}
	states, err := store.UserRepositoryStates(a, 101, []int64{1})
	if err != nil || !states[1].IsFocus || states[1].Note != "first note" {
		t.Fatalf("merged state=%+v %v", states, err)
	}
	if err := store.SetUserRepositoryState(a, 101, 1, false, ""); err != nil {
		t.Fatal(err)
	}
	states, err = store.UserRepositoryStates(a, 101, []int64{1})
	if err != nil || len(states) != 0 {
		t.Fatalf("explicit clear=%+v %v", states, err)
	}
}

package sqlite

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

func TestAgentNonceAtomicAcrossConnectionsAndSurvivesReopen(t *testing.T) {
	store, path := newTestStore(t)
	ctx := context.Background()
	if err := store.PutAuthSession(ctx, authHashFixture(42), authSessionFixture(42, testNow)); err != nil {
		t.Fatal(err)
	}
	key := domain.AgentKey{ID: fmt.Sprintf("rt_ak_%032x", 42), UserID: 42, Name: "Test", Scopes: []string{"repositories:read"}, SigningKey: make([]byte, 32), CreatedAt: testNow, ExpiresAt: testNow.Add(24 * time.Hour)}
	if err := store.PutAgentKey(ctx, key); err != nil {
		t.Fatal(err)
	}
	other, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	var successes, replays atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 16; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			target := store
			if index%2 == 1 {
				target = other
			}
			err := target.AcceptAgentRequest(ctx, key.ID, "durable_nonce_0001", testNow, testNow.Add(6*time.Minute))
			if err == nil {
				successes.Add(1)
			} else if errors.Is(err, domain.ErrAgentReplay) {
				replays.Add(1)
			} else {
				t.Errorf("unexpected nonce error: %v", err)
			}
		}(i)
	}
	group.Wait()
	if successes.Load() != 1 || replays.Load() != 15 {
		t.Fatalf("accepted=%d replay=%d", successes.Load(), replays.Load())
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenWithConfig(ctx, Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if err := reopened.AcceptAgentRequest(ctx, key.ID, "durable_nonce_0001", testNow, testNow.Add(6*time.Minute)); !errors.Is(err, domain.ErrAgentReplay) {
		t.Fatal("reopen erased replay protection")
	}
	if err := reopened.RevokeAgentKey(ctx, 42, key.ID, testNow); err != nil {
		t.Fatal(err)
	}
	if err := store.AcceptAgentRequest(ctx, key.ID, "durable_nonce_0002", testNow, testNow.Add(6*time.Minute)); err == nil {
		t.Fatal("revocation between signature verification and nonce commit accepted")
	}
}

package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
	"github.com/xz1220/repotempo/migrations"
)

func authHashFixture(value int) string { return fmt.Sprintf("%064x", value) }

func authStateFixture(value int, now time.Time) domain.OAuthLoginState {
	return domain.OAuthLoginState{
		StateHash: authHashFixture(value), BindingHash: strings.Repeat("b", 64), Verifier: strings.Repeat("v", 43),
		ReturnPath: "/repositories?tag=skills&lang=zh-CN#project-42", CreatedAt: now, ExpiresAt: now.Add(10 * time.Minute),
	}
}

func authSessionFixture(userID int64, now time.Time) domain.AuthSession {
	return domain.AuthSession{GitHubUserID: userID, Login: "Example-User", CSRFToken: strings.Repeat("c", 64), CreatedAt: now, ExpiresAt: now.Add(12 * time.Hour)}
}

func TestOAuthLoginStateBindingOneTimeConsumptionAndExpiry(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	state := authStateFixture(1, testNow)
	if err := store.PutOAuthLoginState(ctx, state); err != nil {
		t.Fatal(err)
	}
	for _, attempt := range []struct {
		hash, binding string
		now           time.Time
	}{
		{state.StateHash, authHashFixture(9), testNow},
		{authHashFixture(9), state.BindingHash, testNow},
		{state.StateHash, state.BindingHash, testNow.Add(-time.Nanosecond)},
	} {
		got, err := store.ConsumeOAuthLoginState(ctx, attempt.hash, attempt.binding, attempt.now)
		if !errors.Is(err, corestore.ErrNotFound) || got.StateHash != "" {
			t.Fatalf("wrong binding/missing/premature state=%+v %v", got, err)
		}
	}
	got, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, strings.ToUpper(state.BindingHash), testNow)
	if err != nil || !reflect.DeepEqual(got, state) {
		t.Fatalf("state roundtrip failed: %v", err)
	}
	if _, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, state.BindingHash, testNow); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("consumed state replay accepted")
	}
	state = authStateFixture(2, testNow)
	state.ExpiresAt = testNow.Add(time.Nanosecond)
	if err := store.PutOAuthLoginState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, state.BindingHash, state.ExpiresAt); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("state accepted at the exact expiry instant")
	}
	state.StateHash = authHashFixture(3)
	if err := store.PutOAuthLoginState(ctx, state); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, state.BindingHash, testNow); err != nil {
		t.Fatal("sub-millisecond valid lifetime was lost")
	}
}

func TestOAuthStateCapacityCleanupAndCollisionProtection(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	first := authStateFixture(1, testNow)
	if err := store.PutOAuthLoginState(ctx, first); err != nil {
		t.Fatal(err)
	}
	collision := first
	collision.BindingHash = authHashFixture(90)
	collision.Verifier = strings.Repeat("z", 43)
	if err := store.PutOAuthLoginState(ctx, collision); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatal("existing state was overwritten")
	}
	for index := 2; index <= 512; index++ {
		if err := store.PutOAuthLoginState(ctx, authStateFixture(index, testNow)); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.PutOAuthLoginState(ctx, authStateFixture(513, testNow)); !errors.Is(err, domain.ErrAuthCapacity) {
		t.Fatalf("state overflow error=%v", err)
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM oauth_login_states").Scan(&count); err != nil || count != 512 {
		t.Fatalf("state count=%d err=%v", count, err)
	}
	now := testNow.Add(10 * time.Minute)
	store.now = func() time.Time { return now }
	if err := store.PutOAuthLoginState(ctx, authStateFixture(513, now)); err != nil {
		t.Fatal("expired state cleanup did not release capacity:", err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM oauth_login_states").Scan(&count); err != nil || count != 1 {
		t.Fatalf("expired states persisted: %d %v", count, err)
	}
}

func TestAuthSessionsHashOnlyIdentityTTLDeletionAndExpiry(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	const browserToken = "not-a-persisted-browser-token"
	digest := sha256.Sum256([]byte(browserToken))
	hash := hex.EncodeToString(digest[:])
	session := authSessionFixture(123, testNow)
	if err := store.PutAuthSession(ctx, strings.ToUpper(hash), session); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetAuthSession(ctx, hash, testNow)
	if err != nil || !reflect.DeepEqual(got, session) {
		t.Fatalf("auth session roundtrip: %+v %v", got, err)
	}
	var persisted string
	if err := store.db.QueryRow("SELECT token_hash FROM web_auth_sessions").Scan(&persisted); err != nil || persisted != hash || strings.Contains(persisted, browserToken) {
		t.Fatal("session token is not stored as a hash")
	}
	if _, err := store.GetAuthSession(ctx, hash, testNow.Add(-time.Nanosecond)); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("future-created session accepted")
	}
	if _, err := store.GetAuthSession(ctx, hash, session.ExpiresAt); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("expired session accepted")
	}
	if err := store.DeleteAuthSession(ctx, hash); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAuthSession(ctx, hash); err != nil {
		t.Fatal("logout deletion is not idempotent")
	}
	if _, err := store.GetAuthSession(ctx, hash, testNow); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("deleted session accepted")
	}
	session.ExpiresAt = testNow.Add(time.Nanosecond)
	if err := store.PutAuthSession(ctx, hash, session); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetAuthSession(ctx, hash, testNow); err != nil {
		t.Fatal("valid nanosecond session was rounded away")
	}
	if _, err := store.GetAuthSession(ctx, hash, session.ExpiresAt); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("nanosecond session expiry was rounded away")
	}
}

func TestAuthSessionsPerIdentityEvictionExpiryCleanupAndCollisionSafety(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	if err := store.PutAuthSession(ctx, authHashFixture(900), authSessionFixture(2, testNow)); err != nil {
		t.Fatal(err)
	}
	for index := 1; index <= 21; index++ {
		now := testNow.Add(time.Duration(index) * time.Nanosecond)
		store.now = func() time.Time { return now }
		if err := store.PutAuthSession(ctx, authHashFixture(index), authSessionFixture(1, now)); err != nil {
			t.Fatal(err)
		}
	}
	now := testNow.Add(time.Minute)
	if _, err := store.GetAuthSession(ctx, authHashFixture(1), now); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("oldest user's session was not evicted")
	}
	for _, key := range []int{2, 21, 900} {
		if _, err := store.GetAuthSession(ctx, authHashFixture(key), now); err != nil {
			t.Fatalf("newer/other user's session was evicted: %d %v", key, err)
		}
	}
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM web_auth_sessions WHERE github_user_id=1").Scan(&count); err != nil || count != 20 {
		t.Fatalf("per-user session bound=%d %v", count, err)
	}
	collision := authSessionFixture(3, testNow)
	if err := store.PutAuthSession(ctx, authHashFixture(21), collision); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatal("session hash reassigned to a different identity")
	}
	if got, err := store.GetAuthSession(ctx, authHashFixture(21), now); err != nil || got.GitHubUserID != 1 {
		t.Fatal("session identity changed after duplicate token")
	}
	now = testNow.Add(24 * time.Hour)
	store.now = func() time.Time { return now }
	if err := store.PutAuthSession(ctx, authHashFixture(999), authSessionFixture(4, now)); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRow("SELECT COUNT(*) FROM web_auth_sessions").Scan(&count); err != nil || count != 1 {
		t.Fatal("expired sessions were not cleaned across identities")
	}
}

func TestAuthInputValidationDoesNotSerializeOrEchoSecrets(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	state := authStateFixture(1, testNow)
	session := authSessionFixture(1, testNow)
	for _, value := range []any{state, session} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{state.StateHash, state.BindingHash, state.Verifier, session.CSRFToken} {
			if strings.Contains(string(encoded), secret) {
				t.Fatal("authentication secret appeared in JSON")
			}
		}
	}
	for name, modify := range map[string]func(*domain.OAuthLoginState){
		"state hash":             func(s *domain.OAuthLoginState) { s.StateHash = "short-secret" },
		"binding hash":           func(s *domain.OAuthLoginState) { s.BindingHash = strings.Repeat("g", 64) },
		"short verifier":         func(s *domain.OAuthLoginState) { s.Verifier = strings.Repeat("v", 42) },
		"long verifier":          func(s *domain.OAuthLoginState) { s.Verifier = strings.Repeat("v", 129) },
		"nonascii verifier":      func(s *domain.OAuthLoginState) { s.Verifier = strings.Repeat("中", 43) },
		"invalid verifier":       func(s *domain.OAuthLoginState) { s.Verifier = strings.Repeat("v", 42) + "=" },
		"future start":           func(s *domain.OAuthLoginState) { s.CreatedAt = testNow.Add(time.Nanosecond) },
		"expired":                func(s *domain.OAuthLoginState) { s.CreatedAt = testNow.Add(-time.Minute); s.ExpiresAt = testNow },
		"long TTL":               func(s *domain.OAuthLoginState) { s.ExpiresAt = testNow.Add(10*time.Minute + time.Nanosecond) },
		"zero timestamp":         func(s *domain.OAuthLoginState) { s.CreatedAt = time.Time{} },
		"external return":        func(s *domain.OAuthLoginState) { s.ReturnPath = "https://evil.example" },
		"scheme relative return": func(s *domain.OAuthLoginState) { s.ReturnPath = "//evil.example" },
		"encoded authority":      func(s *domain.OAuthLoginState) { s.ReturnPath = "/%2Fevil.example" },
		"backslash return":       func(s *domain.OAuthLoginState) { s.ReturnPath = "/\\evil.example" },
		"encoded backslash":      func(s *domain.OAuthLoginState) { s.ReturnPath = "/%5Cevil.example" },
		"control query":          func(s *domain.OAuthLoginState) { s.ReturnPath = "/repositories?q=%0a" },
	} {
		t.Run("state/"+name, func(t *testing.T) {
			value := state
			modify(&value)
			err := store.PutOAuthLoginState(ctx, value)
			if !errors.Is(err, corestore.ErrInvalid) {
				t.Fatalf("invalid state accepted: %v", err)
			}
			if strings.Contains(err.Error(), value.Verifier) {
				t.Fatal("error echoed verifier")
			}
		})
	}
	for name, modify := range map[string]func(*domain.AuthSession){
		"nonpositive identity": func(s *domain.AuthSession) { s.GitHubUserID = 0 },
		"missing login":        func(s *domain.AuthSession) { s.Login = "" },
		"invalid login":        func(s *domain.AuthSession) { s.Login = "owner/repo" },
		"leading dash":         func(s *domain.AuthSession) { s.Login = "-owner" },
		"csrf":                 func(s *domain.AuthSession) { s.CSRFToken = "csrf-secret" },
		"long TTL":             func(s *domain.AuthSession) { s.ExpiresAt = s.CreatedAt.Add(24*time.Hour + time.Nanosecond) },
		"future":               func(s *domain.AuthSession) { s.CreatedAt = testNow.Add(time.Nanosecond) },
	} {
		t.Run("session/"+name, func(t *testing.T) {
			value := session
			modify(&value)
			err := store.PutAuthSession(ctx, authHashFixture(1), value)
			if !errors.Is(err, corestore.ErrInvalid) {
				t.Fatalf("invalid session accepted: %v", err)
			}
			if strings.Contains(err.Error(), value.CSRFToken) {
				t.Fatal("error echoed CSRF secret")
			}
		})
	}
	for _, hash := range []string{"", strings.Repeat("a", 63), strings.Repeat("z", 64)} {
		if _, err := store.GetAuthSession(ctx, hash, testNow); !errors.Is(err, corestore.ErrInvalid) {
			t.Fatal("invalid hash lookup accepted")
		}
		if err := store.DeleteAuthSession(ctx, hash); !errors.Is(err, corestore.ErrInvalid) {
			t.Fatal("invalid hash deletion accepted")
		}
	}
}

func TestOAuthStateConsumptionIsAtomicAcrossDatabaseConnections(t *testing.T) {
	store, path := newTestStore(t)
	other, err := OpenWithConfig(context.Background(), Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	state := authStateFixture(1, testNow)
	if err := store.PutOAuthLoginState(context.Background(), state); err != nil {
		t.Fatal(err)
	}
	var success atomic.Int32
	var failures atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 16; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			target := store
			if index%2 != 0 {
				target = other
			}
			_, err := target.ConsumeOAuthLoginState(context.Background(), state.StateHash, state.BindingHash, testNow)
			if err == nil {
				success.Add(1)
			} else if errors.Is(err, corestore.ErrNotFound) {
				failures.Add(1)
			} else {
				t.Errorf("concurrent consume: %v", err)
			}
		}(index)
	}
	group.Wait()
	if success.Load() != 1 || failures.Load() != 15 {
		t.Fatalf("state consumed %d times, notfound=%d", success.Load(), failures.Load())
	}
}

func TestAuthCapacityAndSessionEvictionSerializeAcrossConnections(t *testing.T) {
	store, path := newTestStore(t)
	other, err := OpenWithConfig(context.Background(), Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	for index := 1; index <= 511; index++ {
		if err := store.PutOAuthLoginState(context.Background(), authStateFixture(index, testNow)); err != nil {
			t.Fatal(err)
		}
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for index := 512; index < 520; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			target := store
			if index%2 != 0 {
				target = other
			}
			err := target.PutOAuthLoginState(context.Background(), authStateFixture(index, testNow))
			if err == nil {
				successes.Add(1)
			} else if !errors.Is(err, domain.ErrAuthCapacity) {
				t.Errorf("concurrent state capacity: %v", err)
			}
		}(index)
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("capacity allowed %d new states", successes.Load())
	}
	for index := 1; index <= 30; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			target := store
			if index%2 != 0 {
				target = other
			}
			if err := target.PutAuthSession(context.Background(), authHashFixture(index), authSessionFixture(1, testNow)); err != nil {
				t.Errorf("concurrent session: %v", err)
			}
		}(index)
	}
	group.Wait()
	var count int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM web_auth_sessions WHERE github_user_id=1").Scan(&count); err != nil || count != 20 {
		t.Fatalf("concurrent session cap=%d %v", count, err)
	}
}

func TestAuthCancellationAndFailedInsertionRollBackCleanupAndEviction(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.PutOAuthLoginState(canceled, authStateFixture(1, testNow)); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled put=%v", err)
	}
	state := authStateFixture(1, testNow)
	if err := store.PutOAuthLoginState(ctx, state); err != nil {
		t.Fatal(err)
	}
	// Exercise cancellation after a SQL mutation but before commit, not just
	// a context that was already canceled when the operation began.
	inFlight, cancelInFlight := context.WithCancel(ctx)
	err := store.authTransaction(inFlight, func(connection *sql.Conn) error {
		_, err := connection.ExecContext(inFlight, "DELETE FROM oauth_login_states WHERE state_hash=?", state.StateHash)
		cancelInFlight()
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled auth commit: %v", err)
	}
	if _, err := store.ConsumeOAuthLoginState(canceled, state.StateHash, state.BindingHash, testNow); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled consume=%v", err)
	}
	if _, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, state.BindingHash, testNow); err != nil {
		t.Fatal("canceled consumer destroyed valid state")
	}
	for index := 1; index <= 20; index++ {
		if err := store.PutAuthSession(ctx, authHashFixture(index), authSessionFixture(1, testNow)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_auth_insert BEFORE INSERT ON web_auth_sessions BEGIN SELECT RAISE(ABORT,'fixture rejected insert'); END`); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAuthSession(ctx, authHashFixture(21), authSessionFixture(1, testNow)); err == nil {
		t.Fatal("failed session insertion succeeded")
	}
	if _, err := store.GetAuthSession(ctx, authHashFixture(1), testNow); err != nil {
		t.Fatal("failed new session permanently evicted old session")
	}
	if _, err := store.db.Exec("DROP TRIGGER reject_auth_insert"); err != nil {
		t.Fatal(err)
	}
	if err := store.PutAuthSession(ctx, authHashFixture(21), authSessionFixture(1, testNow)); err != nil {
		t.Fatal("failure left pooled connection in a broken transaction:", err)
	}
}

func TestAuthMigrationFailureRollsBackOnlyNewAuthSchema(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0005_repository_tags.sql", "0006_repository_activity.sql"} {
		script, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	// An unexpected pre-existing name makes the second CREATE fail after the
	// first table/index were created. The migration must undo those DDL writes.
	if _, err := db.Exec("PRAGMA user_version=6; CREATE TABLE web_auth_sessions(marker TEXT); INSERT INTO web_auth_sessions VALUES ('preserve')"); err != nil {
		t.Fatal(err)
	}
	before := migrationEvidence(t, db)
	if err := applyMigrations(context.Background(), db); err == nil {
		t.Fatal("conflicting auth schema was silently accepted")
	}
	assertMigrationSettings(t, db, 6, 0)
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name IN ('oauth_login_states','oauth_login_states_expiry')").Scan(&count); err != nil || count != 0 {
		t.Fatal("failed migration left partial auth schema")
	}
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) || !reflect.DeepEqual(migrationRows(t, db, "SELECT * FROM web_auth_sessions"), [][]any{{"preserve"}}) {
		t.Fatal("failed auth migration changed existing data")
	}
}

func TestAuthVerifierUnreservedCharactersAndUTCNormalization(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	zone := time.FixedZone("offset", 8*60*60)
	state := authStateFixture(1, testNow.In(zone))
	state.Verifier = strings.Repeat("a", 39) + "-._~"
	if err := store.PutOAuthLoginState(ctx, state); err != nil {
		t.Fatal("legal PKCE unreserved alphabet rejected:", err)
	}
	got, err := store.ConsumeOAuthLoginState(ctx, state.StateHash, state.BindingHash, testNow)
	if err != nil || got.Verifier != state.Verifier || got.CreatedAt.Location() != time.UTC || !got.ExpiresAt.Equal(state.ExpiresAt) {
		t.Fatal("state UTC normalization changed verifier/time")
	}
	session := authSessionFixture(1, testNow.In(zone))
	if err := store.PutAuthSession(ctx, authHashFixture(1), session); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetAuthSession(ctx, authHashFixture(1), testNow)
	if err != nil || stored.CreatedAt.Location() != time.UTC || !stored.ExpiresAt.Equal(session.ExpiresAt) {
		t.Fatal("session UTC normalization failed")
	}
}

func TestAuthMigrationV6PreservesEveryBusinessTableAndHistoricalOrphan(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"0005_repository_tags.sql", "0006_repository_activity.sql"} {
		script, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(string(script)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("PRAGMA user_version=6; DELETE FROM repository_topics WHERE repository_id>=10155"); err != nil {
		t.Fatal(err)
	}
	before := migrationEvidence(t, db)
	repositories := migrationRows(t, db, "SELECT * FROM repositories ORDER BY github_repo_id")
	if len(before["foreign_key_check"]) != 155 {
		t.Fatal("fixture must preserve the 155 legacy orphan mappings")
	}
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertMigrationSettings(t, db, 9, 0)
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) || !reflect.DeepEqual(repositories, migrationRows(t, db, "SELECT * FROM repositories ORDER BY github_repo_id")) {
		t.Fatal("auth migration changed project/Star/analysis/activity/history data")
	}
	for _, table := range []string{"oauth_login_states", "web_auth_sessions"} {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil || count != 0 {
			t.Fatal("migration invented authentication records")
		}
	}
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
		t.Fatal("repeated auth migration changed business data")
	}
}

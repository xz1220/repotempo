package agentaccess_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
	"github.com/xz1220/repotempo/internal/store/sqlite"
)

func fixture(t *testing.T) (*agentaccess.Service, *sqlite.Store, *time.Time) {
	t.Helper()
	now := time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	store, err := sqlite.OpenWithConfig(context.Background(), sqlite.Config{Path: filepath.Join(t.TempDir(), "agents.db"), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, id := range []int64{42, 99} {
		hash := sha256.Sum256([]byte(fmt.Sprintf("test browser session %d", id)))
		err := store.PutAuthSession(context.Background(), fmt.Sprintf("%x", hash), domain.AuthSession{GitHubUserID: id, Login: fmt.Sprintf("user-%d", id), CSRFToken: fmt.Sprintf("%x", hash), CreatedAt: now, ExpiresAt: now.Add(time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
	}
	service, err := agentaccess.New(store, agentaccess.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, &now
}

func signed(key agentaccess.CreatedKey, target string, at time.Time, nonce string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, "https://repotempo.test"+target, nil)
	stamp := strconv.FormatInt(at.Unix(), 10)
	request.Header.Set("X-RepoTempo-Key", key.Key.ID)
	request.Header.Set("X-RepoTempo-Timestamp", stamp)
	request.Header.Set("X-RepoTempo-Nonce", nonce)
	request.Header.Set("X-RepoTempo-Signature", agentaccess.Sign(key.Secret, request.Method, request.URL.RequestURI(), stamp, nonce, nil))
	return request
}

func TestKeysAreOwnerScopedSecretShownOnceAndRevokedImmediately(t *testing.T) {
	service, store, now := fixture(t)
	ctx := context.Background()
	created, err := service.Create(ctx, 42, "Research", nil, 90)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(created.Secret, "rt_sk_") || len(created.Secret) != 70 || len(created.Key.ID) != 38 || len(created.Key.SigningKey) != 0 {
		t.Fatal("unexpected issued key format")
	}
	stored, err := store.GetAgentKey(ctx, created.Key.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte(created.Secret))
	if string(stored.SigningKey) != string(want[:]) {
		t.Fatal("wrong stored signing key")
	}
	encoded, _ := json.Marshal(stored)
	if strings.Contains(string(encoded), created.Secret) || strings.Contains(string(encoded), fmt.Sprintf("%x", want)) || strings.Contains(string(encoded), "SigningKey") {
		t.Fatal("serialization leaked secret")
	}
	other, err := service.List(ctx, 99)
	if err != nil || len(other) != 0 {
		t.Fatal("key listing crossed users")
	}
	owner, err := service.List(ctx, 42)
	if err != nil || len(owner) != 1 || len(owner[0].SigningKey) != 0 {
		t.Fatal("owner listing lost keys or retained signing credential")
	}
	if err := service.Revoke(ctx, 99, created.Key.ID); !errors.Is(err, agentaccess.ErrInvalid) {
		t.Fatal("another user revoked key")
	}
	request := signed(created, "/api/v1/me", *now, "test_nonce_0000001")
	identity, err := service.Authenticate(ctx, request, nil)
	if err != nil || identity.UserID != 42 || identity.Login != "user-42" || identity.SigningKey != nil {
		t.Fatalf("authentication failed: %v", err)
	}
	if _, err := service.Authenticate(ctx, request, nil); !errors.Is(err, agentaccess.ErrUnauthenticated) {
		t.Fatal("replay succeeded")
	}
	if err := service.Revoke(ctx, 42, created.Key.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, signed(created, "/api/v1/me", *now, "test_nonce_0000002"), nil); !errors.Is(err, agentaccess.ErrUnauthenticated) {
		t.Fatal("revoked key accepted")
	}
}

func TestSignedRequestRejectsTamperingDuplicatesAndExpiry(t *testing.T) {
	service, _, now := fixture(t)
	ctx := context.Background()
	created, err := service.Create(ctx, 42, "Fixture", []string{agentaccess.RepositoriesRead}, 30)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*http.Request){
		"path":   func(r *http.Request) { r.URL.Path = "/api/v1/repositories" },
		"query":  func(r *http.Request) { r.URL.RawQuery = "view=focus" },
		"method": func(r *http.Request) { r.Method = http.MethodPost },
		"timestamp": func(r *http.Request) {
			r.Header.Set("X-RepoTempo-Timestamp", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10))
		},
		"duplicate": func(r *http.Request) { r.Header.Add("X-RepoTempo-Key", created.Key.ID) },
		"nonce":     func(r *http.Request) { r.Header.Set("X-RepoTempo-Nonce", "short") },
		"signature": func(r *http.Request) { r.Header.Set("X-RepoTempo-Signature", strings.Repeat("0", 64)) },
	} {
		t.Run(name, func(t *testing.T) {
			r := signed(created, "/api/v1/me", *now, "test_nonce_0000003")
			mutate(r)
			if _, err := service.Authenticate(ctx, r, nil); !errors.Is(err, agentaccess.ErrUnauthenticated) {
				t.Fatal("tampered request accepted")
			}
		})
	}
	r := signed(created, "/api/v1/me", *now, "test_nonce_0000003")
	if _, err := service.Authenticate(ctx, r, []byte("tampered")); !errors.Is(err, agentaccess.ErrUnauthenticated) {
		t.Fatal("body tampering accepted")
	}
	// Failed signature attempts do not burn an otherwise legitimate nonce.
	if _, err := service.Authenticate(ctx, r, nil); err != nil {
		t.Fatal(err)
	}
	*now = created.Key.ExpiresAt
	if _, err := service.Authenticate(ctx, signed(created, "/api/v1/me", *now, "test_nonce_0000004"), nil); !errors.Is(err, agentaccess.ErrUnauthenticated) {
		t.Fatal("expired key accepted")
	}
}

func TestRateLimitPersistsAcrossConnectionsAndOldTimestamps(t *testing.T) {
	service, store, now := fixture(t)
	ctx := context.Background()
	created, err := service.Create(ctx, 42, "Rate limit", nil, 90)
	if err != nil {
		t.Fatal(err)
	}
	second, err := agentaccess.New(store, agentaccess.Options{Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 120; i++ {
		// Advancing accepted_at makes an old signature expire much sooner
		// than the rolling rate window; cleanup must not erase that history.
		*now = now.Add(10 * time.Millisecond)
		r := signed(created, "/api/v1/me", now.Add(-299*time.Second), fmt.Sprintf("test_nonce_%08d", i))
		if _, err := service.Authenticate(ctx, r, nil); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	*now = now.Add(2 * time.Second)
	if _, err := second.Authenticate(ctx, signed(created, "/api/v1/me", *now, "test_nonce_00000120"), nil); !errors.Is(err, domain.ErrAgentRateLimited) {
		t.Fatalf("rate limit bypass: %v", err)
	}
	*now = now.Add(time.Minute)
	if _, err := second.Authenticate(ctx, signed(created, "/api/v1/me", *now, "test_nonce_00000121"), nil); err != nil {
		t.Fatal("rate window never reopened:", err)
	}
}

func TestCreateValidatesScopesLifetimeAndCapacity(t *testing.T) {
	service, _, _ := fixture(t)
	ctx := context.Background()
	for _, attempt := range []struct {
		name   string
		scopes []string
		days   int
	}{{"", nil, 90}, {"bad\nname", nil, 90}, {"name", []string{"admin:write"}, 90}, {"name", nil, 0}, {"name", nil, 366}} {
		if _, err := service.Create(ctx, 42, attempt.name, attempt.scopes, attempt.days); !errors.Is(err, agentaccess.ErrInvalid) {
			t.Fatal("invalid key settings accepted")
		}
	}
	for i := 0; i < 10; i++ {
		if _, err := service.Create(ctx, 42, "Bounded", nil, 90); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.Create(ctx, 42, "Excess", nil, 90); !errors.Is(err, domain.ErrAgentKeyCapacity) {
		t.Fatal("active key cap bypassed")
	}
	if _, err := service.Create(ctx, 99, "Other user", nil, 90); err != nil {
		t.Fatal("one user exhausted another user's cap")
	}
}

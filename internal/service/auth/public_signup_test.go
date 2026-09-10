package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPublicSignupAllowsOrdinaryUsersWithoutAdminPrivileges(t *testing.T) {
	ctx := context.Background()
	config := testConfig()
	config.AllowPublicSignup = true
	store := newMemoryStore()
	identity := Identity{ID: 77, Login: "ordinary-user"}
	provider := providerFunc(func(context.Context, string, string) (Identity, error) { return identity, nil })
	service, err := New(config, store, Options{Now: func() time.Time { return authNow }, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	start, state := beginForTest(t, service, "/account/api?lang=zh-CN")
	result, err := service.Complete(ctx, state, "code", start.BrowserBinding)
	if err != nil || result.Session.Admin || result.Session.GitHubUserID != 77 || result.ReturnPath != "/account/api?lang=zh-CN" {
		t.Fatalf("ordinary login=%+v %v", result.Session, err)
	}
	session, err := service.Session(ctx, result.Token)
	if err != nil || session.Admin {
		t.Fatalf("ordinary session=%+v %v", session, err)
	}
	// Authorization comes from current deployment config, never persisted role.
	store.mu.Lock()
	saved := store.sessions[secretHash(result.Token)]
	saved.Admin = true
	store.sessions[secretHash(result.Token)] = saved
	store.mu.Unlock()
	session, err = service.Session(ctx, result.Token)
	if err != nil || session.Admin {
		t.Fatal("persisted role escalated ordinary account")
	}
	config.AllowedUserIDs = []int64{77}
	promoted, err := New(config, store, Options{Now: func() time.Time { return authNow }, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	session, err = promoted.Session(ctx, result.Token)
	if err != nil || !session.Admin {
		t.Fatal("current configured admin was not recognized")
	}
	config.AllowedUserIDs = []int64{42}
	config.AllowPublicSignup = false
	restricted, err := New(config, store, Options{Now: func() time.Time { return authNow }, Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restricted.Session(ctx, result.Token); !errors.Is(err, ErrForbidden) {
		t.Fatalf("closed registration retained unauthorized session: %v", err)
	}
}

func TestAPIAccountReturnPathDoesNotAcceptOtherParameters(t *testing.T) {
	list := "/repositories?view=all&size=6&page=2&tag=skills"
	if path, err := SafeReturnPath(list); err != nil || path != list {
		t.Fatalf("paginated login return=%q err=%v", path, err)
	}
	for _, raw := range []string{"/account/api?next=https://evil.example", "/account/api?repository=owner/repo", "/account/api/keys", "/account/api#project-1"} {
		if _, err := SafeReturnPath(raw); !errors.Is(err, ErrInvalidReturn) {
			t.Fatalf("unsafe account return accepted: %s", raw)
		}
	}
}

package app

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
)

func TestAdministratorRepeatedImportPreservesPersonalState(t *testing.T) {
	runtime, _ := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/readme" {
			writeImportReadme(w)
		} else {
			writeImportMetadata(w)
		}
	})
	ctx := context.Background()
	now := runtime.now()
	if err := runtime.store.PutAuthSession(ctx, fmt.Sprintf("%064x", 42), domain.AuthSession{GitHubUserID: 42, Login: "admin", CSRFToken: fmt.Sprintf("%064x", 43), CreatedAt: now, ExpiresAt: now.AddDate(0, 0, 1)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 42, FullName: "owner/repo", Source: domain.DiscoverySourceManual, DiscoveredAt: now}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.store.SetUserRepositoryState(ctx, 42, 42, true, "saved admin note"); err != nil {
		t.Fatal(err)
	}
	admin := domain.WithPrincipal(ctx, domain.Principal{UserID: 42, Admin: true})
	for _, note := range []string{"", "another import note"} {
		if _, err := runtime.EnqueueImport(admin, domain.RepositoryImportRequest{Repository: "owner/repo", Note: note}); err != nil {
			t.Fatal(err)
		}
		if job := processNextImport(t, runtime); job.Stage != "done" {
			t.Fatalf("import=%+v", job)
		}
		state, err := runtime.WatchState(admin, "owner/repo")
		if err != nil || !state.Focus || state.Note != "saved admin note" {
			t.Fatalf("admin duplicate import=%+v %v", state, err)
		}
	}
}

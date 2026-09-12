package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

// Test-only, loopback-only full Runtime fixture. All data and identities are
// created in t.TempDir; no deployed database, account, credential or GitHub API
// is read. Enable explicitly to perform user-facing browser acceptance checks.
func TestBrowserRuntimeFixture(t *testing.T) {
	if os.Getenv("REPOTEMPO_BROWSER_RUNTIME_FIXTURE") != "1" {
		t.Skip("explicit full Runtime browser fixture")
	}
	const base = "http://127.0.0.1:18879"
	f := newPersonalWatchFixture(t, base, true)
	ctx := context.Background()
	for id := int64(1); id <= 28; id++ {
		name := fmt.Sprintf("fixture/project-%02d", id)
		tags := []string{"ai", "tooling"}
		switch id {
		case 1:
			name = "fixture/audio.cpp"
			tags = []string{"ai", "audio", "cpp"}
		case 2:
			name = "fixture/agent-code"
			tags = []string{"ai", "python"}
		case 3:
			name = "fixture/codex-tools"
			tags = []string{"developer-tools", "python"}
		}
		description := "Isolated browser acceptance data for search, tags, pagination and account state."
		language := "Go"
		discovered := f.now.AddDate(0, 0, -65)
		if id > 24 {
			discovered = f.now
		}
		_, _, err := f.runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: id, FullName: name, HTMLURL: "https://github.com/" + name, Description: &description, PrimaryLanguage: &language, GitHubTopics: &tags, Source: domain.DiscoverySourceManual, DiscoveredAt: discovered})
		if err != nil {
			t.Fatal(err)
		}
		for _, days := range []int{0, 1, 2, 7, 14, 30, 60} {
			if id > 24 && days > 0 {
				continue
			}
			at := f.now.AddDate(0, 0, -days)
			stars := int64(3000) + id*100 - int64(days)*(29-id)
			if _, err := f.runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: id, SnapshotDate: domain.ShanghaiDate(at), CapturedAt: at, StarCount: &stars, FetchStatus: domain.FetchSuccess}); err != nil {
				t.Fatal(err)
			}
		}
	}
	f.session(t, 101, 1)
	f.session(t, 202, 1)
	if err := f.runtime.store.SetUserRepositoryState(ctx, 101, 1, true, "Keep this private audio research note."); err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.store.SetUserRepositoryState(ctx, 202, 2, true, "Other account note."); err != nil {
		t.Fatal(err)
	}
	serial := 10
	mux := http.NewServeMux()
	mux.HandleFunc("GET /fixture/sign-in", func(w http.ResponseWriter, r *http.Request) {
		userID := int64(101)
		if r.URL.Query().Get("user") == "202" {
			userID = 202
		}
		if r.URL.Query().Get("user") == "42" {
			userID = 42
		}
		serial++
		http.SetCookie(w, f.session(t, userID, serial))
		http.Redirect(w, r, "/repositories?view=all&new=0&date=2026-09-12&lang=en", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /fixture/state", func(w http.ResponseWriter, r *http.Request) {
		userID := int64(101)
		if r.URL.Query().Get("user") == "202" {
			userID = 202
		}
		repositoryID, _ := strconv.ParseInt(r.URL.Query().Get("repository_id"), 10, 64)
		if repositoryID == 0 {
			repositoryID = 1
		}
		state, err := f.runtime.store.UserRepositoryStates(r.Context(), userID, []int64{repositoryID})
		if err != nil {
			http.Error(w, "fixture state unavailable", 500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(state[repositoryID])
	})
	mux.Handle("/", f.handler)
	listener, err := net.Listen("tcp", "127.0.0.1:18879")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	defer server.Close()
	go server.Serve(listener)
	t.Log("Full Runtime fixture ready: " + base + "/fixture/sign-in; state: /fixture/state")
	timer := time.NewTimer(30 * time.Minute)
	defer timer.Stop()
	<-timer.C
}

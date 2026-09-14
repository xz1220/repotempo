package snapshot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
)

func TestRunContinuesPastBlockedRepositoryAndResumesFailedSnapshot(t *testing.T) {
	first, second := testRepository(), testRepository()
	second.GitHubRepoID, second.FullName = 43, "owner/second"
	store := newFakeSnapshotStore(first)
	store.repositories = []domain.Repository{first, second}
	blocked := true
	calls := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls[r.URL.Path]++
		w.Header().Set("X-RateLimit-Remaining", "4900")
		if r.URL.Path == "/repositories/42" && blocked {
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, `{"message":"Repository access blocked","block":{"reason":"tos"}}`)
			return
		}
		id, name := int64(42), "owner/repo"
		if r.URL.Path == "/repositories/43" {
			id, name = 43, "owner/second"
		}
		fmt.Fprintf(w, `{"id":%d,"full_name":%q,"html_url":"https://github.com/%s","stargazers_count":1234,"topics":[]}`, id, name, name)
	}))
	defer server.Close()
	client, err := github.NewClient(github.ClientOptions{BaseURL: server.URL, UserAgent: "snapshot-test", HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	service := Service{Store: store, GitHub: client, Now: func() time.Time { return time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC) }}
	report, err := service.Run(context.Background(), nil)
	if err != nil || report.TargetCount != 2 || report.SuccessCount != 1 || report.FailureCount != 1 {
		t.Fatalf("blocked repository stopped batch: %+v %v", report, err)
	}
	failed, good := store.today[42], store.today[43]
	if failed.FetchStatus != domain.FetchFailed || failed.StarCount != nil || failed.ErrorCode != "repository_access_blocked" || good.StarCount == nil || *good.StarCount != 1234 {
		t.Fatalf("wrong failure or success: %+v %+v", failed, good)
	}
	if len(store.monitoring) != 0 || len(store.githubStatus) != 0 {
		t.Fatal("blocked response changed repository lifecycle")
	}
	blocked = false
	report, err = service.Run(context.Background(), nil)
	if err != nil || report.SuccessCount != 1 || report.SkippedCount != 1 || report.FailureCount != 0 || calls["/repositories/43"] != 1 {
		t.Fatalf("same-day recovery failed: %+v calls=%v err=%v", report, calls, err)
	}
	if store.today[42].FetchStatus != domain.FetchSuccess {
		t.Fatal("failed observation was not repaired")
	}
}

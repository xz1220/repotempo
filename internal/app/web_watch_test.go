package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebWatchEndToEndAddsPublicRepositoryAndImmediateSnapshot(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	var apiCalls int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls++
		if r.Method != http.MethodGet || r.URL.Path != "/repos/owner/interesting-agent" {
			t.Errorf("unexpected GitHub operation: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("public metadata request unexpectedly required a token")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "full_name": "owner/interesting-agent", "html_url": "https://github.com/owner/interesting-agent", "description": "An AI coding assistant for small teams", "stargazers_count": 456, "private": false, "topics": []string{"coding-agent"}})
	}))
	defer upstream.Close()
	client, err := github.NewClient(github.ClientOptions{BaseURL: upstream.URL, UserAgent: "github-radar-watch-test", HTTPClient: upstream.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := OpenRuntimeWithOptions(ctx, Settings{DatabasePath: t.TempDir() + "/radar.db"}, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	runtime.loaded, runtime.github, runtime.discovery = true, client, &config.Discovery{}
	// A single manual observation must not advance the dashboard's daily batch
	// date, but it must be immediately accessible from the add redirect.
	yesterday := now.AddDate(0, 0, -1)
	oldRepo, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 1, FullName: "owner/already-tracked", Source: domain.DiscoverySourceGitHubSearch,
		DiscoveredAt: yesterday.AddDate(0, 0, -10),
	})
	if err != nil {
		t.Fatal(err)
	}
	oldStars := int64(100)
	if _, err := runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: oldRepo.GitHubRepoID, SnapshotDate: domain.ShanghaiDate(yesterday), CapturedAt: yesterday, StarCount: &oldStars, FetchStatus: domain.FetchSuccess}); err != nil {
		t.Fatal(err)
	}
	finished := yesterday.Add(time.Minute)
	if err := runtime.store.CreateJobRun(ctx, domain.JobRun{
		RunID: "completed-yesterday", JobType: "snapshot", Status: domain.JobSuccess, StartedAt: yesterday, FinishedAt: &finished,
		TargetCount: 1, SuccessCount: 1, Details: json.RawMessage(`{"date":"2026-09-06"}`),
	}); err != nil {
		t.Fatal(err)
	}
	_, _, err = runtime.store.UpsertTopic(ctx, domain.Topic{Slug: "coding-agents", Name: "编程助手", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := web.New(WebAdapter{Store: runtime.store}, web.Options{Watcher: runtime, AllowLocalWrites: true, Locale: "zh-CN", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	base := "http://127.0.0.1:8878"
	form := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, base+"/watch/new?lang=zh-CN", nil)
	request.RemoteAddr = "127.0.0.1:50201"
	handler.ServeHTTP(form, request)
	var cookie *http.Cookie
	for _, candidate := range form.Result().Cookies() {
		if candidate.Name == "github_radar_watch_csrf" {
			cookie = candidate
		}
	}
	if cookie == nil {
		t.Fatal("missing form CSRF token")
	}
	values := url.Values{"repository": {"https://github.com/owner/interesting-agent"}, "note": {"值得长期研究"}, "csrf_token": {cookie.Value}}
	request = httptest.NewRequest(http.MethodPost, base+"/watch?lang=zh-CN", strings.NewReader(values.Encode()))
	request.RemoteAddr = "127.0.0.1:50201"
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", base)
	request.AddCookie(cookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/repositories/99?lang=zh-CN" {
		t.Fatalf("add did not redirect to detail: %d %s", response.Code, response.Body.String())
	}
	if apiCalls != 1 {
		t.Fatalf("GitHub requests = %d, want one metadata fetch", apiCalls)
	}
	repository, err := runtime.store.GetRepository(ctx, 99)
	if err != nil || !repository.IsFocus || repository.ManualNote != "值得长期研究" {
		t.Fatalf("stored repository: %#v, %v", repository, err)
	}
	snapshot, err := runtime.store.GetDailySnapshot(ctx, 99, domain.ShanghaiDate(now))
	if err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 456 || snapshot.FetchStatus != domain.FetchSuccess {
		t.Fatalf("stored snapshot: %#v, %v", snapshot, err)
	}
	topics, err := runtime.store.ListRepositoryTopics(ctx, 99)
	if err != nil || len(topics) != 1 || topics[0].Slug != "coding-agents" {
		t.Fatalf("automatic classification: %#v, %v", topics, err)
	}
	preferred, err := runtime.store.PreferredSnapshotDate(ctx)
	if err != nil || preferred != domain.ShanghaiDate(yesterday) {
		t.Fatalf("manual add changed the dashboard batch date: %s, %v", preferred, err)
	}
	adapter := WebAdapter{Store: runtime.store}
	detail, err := adapter.GetRepositoryDetail(ctx, 99, time.Time{})
	if err != nil || domain.ShanghaiDate(detail.AsOf) != domain.ShanghaiDate(now) || len(detail.History) != 1 {
		t.Fatalf("new project detail hidden behind yesterday's batch: %#v, %v", detail, err)
	}
	if _, err := adapter.GetRepositoryDetail(ctx, 99, yesterday); !errors.Is(err, web.ErrNotFound) {
		t.Fatalf("explicit date before first discovery must remain absent: %v", err)
	}
	for _, path := range []string{"/repositories/99?lang=zh-CN", "/repositories?q=interesting-agent&lang=zh-CN", "/repositories?focus=1&lang=zh-CN", "/repositories?q=interesting-agent&date=2026-09-07&lang=zh-CN"} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+path, nil))
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "owner/interesting-agent") || !strings.Contains(response.Body.String(), "456") {
			t.Fatalf("added project not immediately visible on %s: status %d", path, response.Code)
		}
	}
	for _, test := range []struct {
		path   string
		status int
	}{
		{"/repositories/99?date=2026-09-06&lang=zh-CN", http.StatusNotFound},
		{"/repositories/99?date=2026-09-07&lang=zh-CN", http.StatusOK},
		{"/repositories/99?date=invalid&lang=zh-CN", http.StatusBadRequest},
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, base+test.path, nil))
		if response.Code != test.status {
			t.Fatalf("historical detail %s returned %d, want %d", test.path, response.Code, test.status)
		}
	}
	// An already tracked project can also receive a newer manual observation;
	// its old first_seen date alone must not decide the default detail cutoff.
	refreshedStars := int64(110)
	if _, err := runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: oldRepo.GitHubRepoID, SnapshotDate: domain.ShanghaiDate(now), CapturedAt: now, StarCount: &refreshedStars, FetchStatus: domain.FetchSuccess}); err != nil {
		t.Fatal(err)
	}
	refreshed, err := adapter.GetRepositoryDetail(ctx, oldRepo.GitHubRepoID, time.Time{})
	if err != nil || refreshed.Repository.CurrentStars == nil || *refreshed.Repository.CurrentStars != 110 || domain.ShanghaiDate(refreshed.AsOf) != domain.ShanghaiDate(now) {
		t.Fatalf("newer project snapshot hidden behind batch: %#v, %v", refreshed, err)
	}
	historical, err := adapter.GetRepositoryDetail(ctx, oldRepo.GitHubRepoID, yesterday)
	if err != nil || historical.Repository.CurrentStars == nil || *historical.Repository.CurrentStars != 100 || len(historical.History) != 1 {
		t.Fatalf("historical detail leaked a newer observation: %#v, %v", historical, err)
	}
}

func TestRepositoryDetailDefaultsToFirstSeenWhenSnapshotIsNotAvailable(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 8, 0, 0, 0, time.UTC)
	runtime, err := OpenRuntimeWithOptions(ctx, Settings{DatabasePath: t.TempDir() + "/radar.db"}, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	yesterday := now.AddDate(0, 0, -1)
	finished := yesterday.Add(time.Minute)
	if err := runtime.store.CreateJobRun(ctx, domain.JobRun{RunID: "completed", JobType: "snapshot", Status: domain.JobSuccess, StartedAt: yesterday, FinishedAt: &finished, Details: json.RawMessage(`{"date":"2026-09-06"}`)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 99, FullName: "owner/newly-imported", Source: domain.DiscoverySourceLegacy, DiscoveredAt: now}); err != nil {
		t.Fatal(err)
	}
	adapter := WebAdapter{Store: runtime.store}
	detail, err := adapter.GetRepositoryDetail(ctx, 99, time.Time{})
	if err != nil || domain.ShanghaiDate(detail.AsOf) != domain.ShanghaiDate(now) || detail.Repository.ID != 99 {
		t.Fatalf("first-seen fallback failed: %#v, %v", detail, err)
	}
	if detail.Repository.CurrentStars != nil {
		t.Fatal("missing snapshot must not invent a star count")
	}
	if _, err := adapter.GetRepositoryDetail(ctx, 99, yesterday); !errors.Is(err, web.ErrNotFound) {
		t.Fatalf("explicit history cutoff ignored: %v", err)
	}
}

func TestWebWatchGitHubErrorsStaySanitized(t *testing.T) {
	for _, test := range []struct {
		status int
		want   error
	}{{http.StatusNotFound, web.ErrWatchPrivate}, {http.StatusTooManyRequests, web.ErrWatchRateLimited}, {http.StatusBadGateway, web.ErrWatchUnavailable}} {
		t.Run(fmt.Sprint(test.status), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, `{"message":"fixture-sensitive-upstream-context"}`)
			}))
			defer upstream.Close()
			client, err := github.NewClient(github.ClientOptions{BaseURL: upstream.URL, UserAgent: "github-radar-watch-test", HTTPClient: upstream.Client(), MaxRetries: 0})
			if err != nil {
				t.Fatal(err)
			}
			runtime, err := OpenRuntime(context.Background(), Settings{DatabasePath: t.TempDir() + "/radar.db"})
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			runtime.loaded, runtime.github, runtime.discovery = true, client, &config.Discovery{}
			_, err = runtime.AddWatch(context.Background(), web.WatchRequest{Repository: "owner/repo"})
			if err != test.want {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

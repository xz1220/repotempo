package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

func TestWebProjectionKeepsEveryUserAndAnonymousSeparate(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
	runtime, err := OpenRuntimeWithOptions(ctx, Settings{DatabasePath: t.TempDir() + "/users.db"}, RuntimeOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	for _, id := range []int64{42, 101, 202} {
		if err := runtime.store.PutAuthSession(ctx, fmt.Sprintf("%064x", id), domain.AuthSession{GitHubUserID: id, Login: fmt.Sprintf("user-%d", id), CSRFToken: strings.Repeat("c", 64), CreatedAt: now, ExpiresAt: now.Add(time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	topic, _, err := runtime.store.UpsertTopic(ctx, domain.Topic{Slug: "tools", Name: "Tools", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	legacyNote := "legacy owner secret copied to analysis"
	for id := int64(1); id <= 2; id++ {
		_, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: id, FullName: fmt.Sprintf("owner/project-%d", id), Source: domain.DiscoverySourceManual, DiscoveredAt: now.AddDate(0, 0, -10), ManualNote: &legacyNote})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runtime.store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: id, TopicID: topic.ID, Source: domain.TopicSourceManual, AssignedAt: now}); err != nil {
			t.Fatal(err)
		}
		source := "manual_note"
		if id == 2 {
			source = "imported"
		} // migration 0003 copied manual notes under this source
		if _, err := runtime.store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: id, SummaryZH: legacyNote, Source: source}); err != nil {
			t.Fatal(err)
		}
		for _, day := range []int{0, -7} {
			stars := int64(100+day) * id
			if _, err := runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: id, SnapshotDate: domain.ShanghaiDate(now.AddDate(0, 0, day)), CapturedAt: now, StarCount: &stars, FetchStatus: domain.FetchSuccess}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := runtime.store.SetRepositoryFocus(ctx, 1, true); err != nil {
		t.Fatal(err)
	}
	if err := runtime.store.AdoptLegacyWorkspace(ctx, 42); err != nil {
		t.Fatal(err)
	}
	a := domain.WithPrincipal(ctx, domain.Principal{UserID: 101, Login: "first"})
	b := domain.WithPrincipal(ctx, domain.Principal{UserID: 202, Login: "second"})
	anonymous := domain.WithPrincipal(ctx, domain.Principal{})
	if _, err := runtime.AddWatch(a, web.WatchRequest{Repository: "owner/project-1", Focus: true, Note: "A private note"}); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.AddWatch(b, web.WatchRequest{Repository: "owner/project-2", Focus: true, Note: "B private note"}); err != nil {
		t.Fatal(err)
	}
	state, err := runtime.WatchState(a, "owner/project-1")
	if err != nil || !state.Focus || state.Note != "A private note" {
		t.Fatalf("own form prefill: %+v %v", state, err)
	}
	state, err = runtime.WatchState(b, "owner/project-1")
	if err != nil || state.Focus || state.Note != "" {
		t.Fatalf("form prefill leaked: %+v %v", state, err)
	}
	adapter := WebAdapter{Store: runtime.store}
	for _, item := range []struct {
		name       string
		ctx        context.Context
		own, other string
		focusID    int64
	}{{"A", a, "A private note", "B private note", 1}, {"B", b, "B private note", "A private note", 2}, {"anonymous", anonymous, "", "private note", 0}} {
		t.Run(item.name, func(t *testing.T) {
			page, err := adapter.ListRepositoryTrends(item.ctx, web.RepositoryQuery{AsOf: now, WindowDays: 7, Limit: 20})
			if err != nil || len(page.Items) != 2 {
				t.Fatalf("page: %+v %v", page, err)
			}
			for _, metric := range page.Items {
				if metric.IsFocus != (metric.ID == item.focusID) {
					t.Fatalf("wrong focus owner: %+v", metric)
				}
			}
			calls := []func() (any, error){
				func() (any, error) { return page, nil },
				func() (any, error) { return adapter.GetRepositoryDetail(item.ctx, 1, now) },
				func() (any, error) {
					return adapter.ListRepositoryMetrics(item.ctx, web.RepositoryQuery{AsOf: now, Limit: 20})
				},
				func() (any, error) { return adapter.DashboardSummary(item.ctx, now) },
				func() (any, error) { return adapter.GetTopicDetail(item.ctx, "tools", now, false) },
				func() (any, error) {
					return adapter.RadarOverview(item.ctx, web.RepositoryQuery{AsOf: now, WindowDays: 7})
				},
			}
			for index, call := range calls {
				value, err := call()
				if err != nil {
					t.Fatalf("projection %d: %v", index, err)
				}
				encoded, _ := json.Marshal(value)
				if strings.Contains(string(encoded), legacyNote) || strings.Contains(string(encoded), item.other) {
					t.Fatalf("projection %d leaked another account: %s", index, encoded)
				}
			}
			if item.own != "" {
				encoded, _ := json.Marshal(page)
				if !strings.Contains(string(encoded), item.own) {
					t.Fatal("own note missing")
				}
			}
		})
	}
	if _, err := runtime.AddWatch(a, web.WatchRequest{Repository: "owner/new"}); !errors.Is(err, web.ErrWatchAdminRequired) {
		t.Fatalf("ordinary user ingested new catalogue repo: %v", err)
	}
	if _, err := runtime.EnqueueImport(a, domain.RepositoryImportRequest{Repository: "owner/new", OwnerUserID: 42}); !errors.Is(err, domain.ErrImportInvalid) {
		t.Fatalf("ordinary user spoofed import owner: %v", err)
	}
	if _, err := runtime.EnqueueImport(anonymous, domain.RepositoryImportRequest{Repository: "owner/new"}); !errors.Is(err, domain.ErrImportInvalid) {
		t.Fatalf("anonymous queued import: %v", err)
	}
	if _, err := adapter.ListJobRuns(a, 20, 0); !errors.Is(err, web.ErrNotFound) {
		t.Fatalf("ordinary user read global job data: %v", err)
	}
	if err := runtime.SetRepositoryFocus(anonymous, 1, false); !errors.Is(err, web.ErrFocusInvalid) {
		t.Fatalf("anonymous focus write accepted: %v", err)
	}
	if runtime.loaded {
		t.Fatal("personal writes triggered global upstream dependencies")
	}
}

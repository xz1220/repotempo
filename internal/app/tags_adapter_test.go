package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/store/sqlite"
	"github.com/xz1220/repotempo/internal/web"
)

func TestTagAdapterShowsRawLabelsAndGlobalOptionsForANarrowDailyFeed(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 8, 4, 0, 0, 0, time.UTC)
	store, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/tags.db", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, input := range []struct {
		id               int64
		name             string
		github, research []string
		old              bool
	}{
		{1, "owner/new", []string{"unknown-github-label", "Agent"}, []string{"投资", "agent"}, false},
		{2, "owner/old", []string{"skills"}, []string{"数据"}, true},
	} {
		seen := now
		if input.old {
			seen = now.AddDate(0, 0, -30)
		}
		if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: input.id, FullName: input.name, Source: domain.DiscoverySourceGitHubSearch, DiscoveredAt: seen, GitHubTopics: &input.github, ResearchTags: &input.research}); err != nil {
			t.Fatal(err)
		}
	}
	category, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "coding-agents", Name: "Coding Agents", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: 1, TopicID: category.ID, Source: domain.TopicSourceManual}); err != nil {
		t.Fatal(err)
	}
	adapter := WebAdapter{Store: store}
	page, err := adapter.ListRepositoryTrends(ctx, web.RepositoryQuery{AsOf: now, Tag: "　投资　", OnlyNew: true, Limit: 20})
	if err != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != 1 {
		t.Fatalf("filtered daily feed = %+v, %v", page, err)
	}
	wantRaw := []string{"unknown-github-label", "agent", "投资"}
	if !reflect.DeepEqual(page.Items[0].Tags, wantRaw) {
		t.Fatalf("card raw tags = %+v", page.Items[0].Tags)
	}
	if len(page.Items[0].Topics) != 1 || page.Items[0].Topics[0].Slug != "coding-agents" {
		t.Fatal("taxonomy and author labels were mixed or lost")
	}
	options := map[string]int{}
	for _, tag := range page.Tags {
		options[tag.Name] = tag.Count
	}
	for _, name := range []string{"skills", "数据", "投资", "agent", "coding-agents", "coding agents", "unknown-github-label"} {
		if options[name] != 1 {
			t.Fatalf("global option %q was hidden by today's filter: %+v", name, options)
		}
	}
	if page.Filter.Tag != "投资" {
		t.Fatalf("adapter did not normalize the selected tag: %q", page.Filter.Tag)
	}
	detail, err := adapter.GetRepositoryDetail(ctx, 1, now)
	if err != nil || !reflect.DeepEqual(detail.Repository.Tags, wantRaw) {
		t.Fatalf("detail lost real labels: %+v, %v", detail.Repository.Tags, err)
	}
	empty, err := adapter.ListRepositoryTrends(ctx, web.RepositoryQuery{AsOf: now, Tag: "not-present", OnlyNew: true})
	if err != nil || len(empty.Items) != 0 || len(empty.Tags) != len(options) {
		t.Fatalf("empty filtered feed lost globally available tags: %+v, %v", empty, err)
	}
}

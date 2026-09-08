package discovery

import (
	"context"
	"github.com/xz1220/repotempo/internal/source"
	"testing"
)

func TestRegistryCarriesRawTopicsAndResearchTagsWithoutTaxonomyConversion(t *testing.T) {
	store := &fakeRepositoryStore{}
	registry := Registry{Store: store}
	_, err := registry.Merge(context.Background(), []source.Candidate{
		{Source: "github_trending", Repository: source.Repository{ID: 1, FullName: "o/empty", Topics: []string{}}},
		{Source: "legacy", Repository: source.Repository{ID: 2, FullName: "o/unknown", ResearchTags: []string{"中文研究"}}},
	})
	if err != nil || len(store.observations) != 2 {
		t.Fatalf("registry observations=%v err=%v", store.observations, err)
	}
	if store.observations[0].GitHubTopics == nil || *store.observations[0].GitHubTopics == nil || len(*store.observations[0].GitHubTopics) != 0 {
		t.Fatal("registry lost known-empty topics")
	}
	if store.observations[1].GitHubTopics != nil || store.observations[1].ResearchTags == nil || (*store.observations[1].ResearchTags)[0] != "中文研究" {
		t.Fatal("registry confused missing GitHub topics and research tags")
	}
}

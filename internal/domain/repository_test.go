package domain

import "testing"

func TestGitHubTrendingIsAValidDiscoverySource(t *testing.T) {
	if !DiscoverySourceGitHubTrending.Valid() || string(DiscoverySourceGitHubTrending) != "github_trending" {
		t.Fatal("GitHub Trending discovery source is not valid")
	}
	if DiscoverySource("invented_source").Valid() {
		t.Fatal("unknown discovery source is valid")
	}
}

package source

import (
	"testing"
	"time"
)

func TestMergeCandidatesUsesPermanentIDAndPreservesFirstProvenance(t *testing.T) {
	stars := int64(10)
	firstSeen := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	merged := MergeCandidates(
		[]Candidate{{Repository: Repository{ID: 1, FullName: "old/name"}, Source: "legacy", DiscoveredAt: firstSeen}},
		[]Candidate{{Repository: Repository{ID: 1, FullName: "new/name", AbsoluteStars: &stars}, Source: "github_search", Profile: "recent", IsFocus: true}},
	)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	candidate := merged[0]
	if candidate.Source != "legacy" || candidate.DiscoveredAt != firstSeen || candidate.Repository.FullName != "new/name" || candidate.Repository.AbsoluteStars == nil || !candidate.IsFocus {
		t.Fatalf("candidate = %+v", candidate)
	}
	if len(candidate.PreviousNames) != 1 || candidate.PreviousNames[0] != "old/name" {
		t.Fatalf("previous names = %v", candidate.PreviousNames)
	}
	if got, want := candidate.Metadata["discovery_sources"], "github_search,legacy"; got != want {
		t.Fatalf("discovery sources = %q, want %q", got, want)
	}
}

func TestMergeCandidatesDoesNotLetLegacyNameRegressGitHubName(t *testing.T) {
	merged := MergeCandidates(
		[]Candidate{{Repository: Repository{ID: 1, FullName: "current/name"}, Source: "github_search"}},
		[]Candidate{{Repository: Repository{ID: 1, FullName: "old/name"}, Source: "legacy"}},
	)
	if len(merged) != 1 || merged[0].Repository.FullName != "current/name" {
		t.Fatalf("merged = %+v", merged)
	}
	if len(merged[0].PreviousNames) != 1 || merged[0].PreviousNames[0] != "old/name" {
		t.Fatalf("previous names = %v", merged[0].PreviousNames)
	}
}

func TestMergeCandidatesRetainsSourcesAcrossRepeatedMerges(t *testing.T) {
	first := MergeCandidates(
		[]Candidate{{Repository: Repository{ID: 1, FullName: "owner/repo"}, Source: "github_trending"}},
		[]Candidate{{Repository: Repository{ID: 1, FullName: "owner/repo"}, Source: "github_search"}},
	)
	for range 3 {
		first = MergeCandidates(first)
		if len(first) != 1 || first[0].Source != "github_trending" || first[0].Metadata["discovery_sources"] != "github_search,github_trending" {
			t.Fatalf("repeated merge lost discovery provenance: %+v", first)
		}
	}
}

func TestVerifiedTrendingIdentityHasGitHubAPIPriority(t *testing.T) {
	merged := MergeCandidates(
		[]Candidate{{Repository: Repository{ID: 1, FullName: "github/current"}, Source: "github_trending"}},
		[]Candidate{{Repository: Repository{ID: 1, FullName: "legacy/old"}, Source: "legacy"}},
		[]Candidate{{Repository: Repository{ID: 1, FullName: "oss/old"}, Source: "ossinsight"}},
	)
	if len(merged) != 1 || merged[0].Repository.FullName != "github/current" || merged[0].Source != "github_trending" {
		t.Fatalf("verified GitHub Trending identity was replaced by older sources: %#v", merged)
	}
	if sourcePriority("github_trending") != sourcePriority("github_search") {
		t.Fatal("API-verified Trending and Search identities should have equal priority")
	}
	if merged[0].Metadata["discovery_sources"] != "github_trending,legacy,ossinsight" {
		t.Fatalf("Trending provenance was lost: %#v", merged[0].Metadata)
	}
}

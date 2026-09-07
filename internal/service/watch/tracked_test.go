package watch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/source"
	"github.com/xz1220/github-radar/internal/store/sqlite"
)

type trackedResolver struct {
	repository source.Repository
	requests   []string
}

func (r *trackedResolver) ResolveRepository(_ context.Context, fullName string, _ int64) (source.Repository, error) {
	r.requests = append(r.requests, fullName)
	return r.repository, nil
}

func TestNormalizeRepositoryRejectsURLsOutsideCanonicalGitHubRepo(t *testing.T) {
	for input, expected := range map[string]string{"owner/repo": "owner/repo", " https://github.com/owner/repo/ ": "owner/repo", "https://github.com/owner/repo.git": "owner/repo"} {
		value, err := NormalizeRepository(input)
		if err != nil || value != expected {
			t.Fatalf("%q -> %q, %v", input, value, err)
		}
	}
	for _, input := range []string{"", "owner", "owner/repo/issues", "http://github.com/owner/repo", "https://evil.example/owner/repo", "https://github.com.evil.example/owner/repo", "https://github.com:443/owner/repo", "https://secret@github.com/owner/repo", "https://github.com/owner/repo?token=abc", "https://github.com/owner%2Frepo", "https://github.com/owner/../repo", "owner/..", "owner/.", "owner/name#issue"} {
		if _, err := NormalizeRepository(input); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
}

func TestTrackedAddIsAtomicAndPreservesExistingEvidence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 7, 16, 10, 0, 0, time.UTC)
	db, err := sqlite.OpenWithConfig(ctx, sqlite.Config{Path: t.TempDir() + "/watch.db", Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	topic, _, err := db.UpsertTopic(ctx, domain.Topic{Slug: "coding-agents", Name: "Coding agents", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	stars := int64(12)
	resolver := &trackedResolver{repository: source.Repository{ID: 99, FullName: "owner/canonical", HTMLURL: "https://github.com/owner/canonical", AbsoluteStars: &stars}}
	service := Service{Store: db, Resolver: resolver, Now: func() time.Time { return now }}
	result, err := service.AddTracked(ctx, "https://github.com/owner/old-name", "my original note", "coding-agents")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || !result.Repository.IsFocus || result.Repository.FullName != "owner/canonical" {
		t.Fatalf("unexpected result %#v", result)
	}
	day := domain.ShanghaiDate(now)
	if day != "2026-09-08" {
		t.Fatal("test must exercise Shanghai date boundary")
	}
	snapshot, err := db.GetDailySnapshot(ctx, 99, day)
	if err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 12 || !snapshot.CapturedAt.Equal(now) {
		t.Fatalf("first observation %#v, %v", snapshot, err)
	}
	firstSeen := result.Repository.FirstSeenAt
	stars = 200
	now = now.Add(time.Hour)
	result, err = service.AddTracked(ctx, "owner/canonical", "replacement should not overwrite", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Created || result.Repository.ManualNote != "my original note" || !result.Repository.FirstSeenAt.Equal(firstSeen) {
		t.Fatal("duplicate changed original evidence")
	}
	snapshot, err = db.GetDailySnapshot(ctx, 99, day)
	if err != nil || *snapshot.StarCount != 12 {
		t.Fatal("successful daily Star observation overwritten")
	}
	assignments, err := db.ListRepositoryTopicAssignments(ctx, &result.Repository.GitHubRepoID)
	if err != nil || len(assignments) != 1 || assignments[0].TopicID != topic.ID || assignments[0].Source != domain.TopicSourceManual {
		t.Fatal("manual topic lost")
	}
	resolver.repository.ID = 100
	resolver.repository.FullName = "owner/another"
	if _, err := service.AddTracked(ctx, "owner/another", "", "nonexistent"); err == nil {
		t.Fatal("invalid topic accepted")
	}
	if _, err := db.GetRepository(ctx, 100); err == nil {
		t.Fatal("failed add partially persisted repository")
	}
	resolver.repository.Private = true
	if _, err := service.AddTracked(ctx, "owner/private", "", ""); !errors.Is(err, ErrPrivateRepository) {
		t.Fatalf("private repository accepted: %v", err)
	}
	if _, err := db.GetRepository(ctx, 100); err == nil {
		t.Fatal("private repository persisted")
	}
	resolver.repository.Private = false
	resolver.repository.AbsoluteStars = nil
	if _, err := service.AddTracked(ctx, "owner/another", "", ""); !errors.Is(err, ErrMissingStars) {
		t.Fatal("missing stars accepted")
	}
}

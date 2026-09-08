package sqlite

import (
	"context"
	"database/sql"
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/migrations"
)

func TestRepositoryTagsRoundTripAndAbsentObservationsPreserveMetadata(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	repository := addRepository(t, store, 1, "owner/tags")
	if repository.GitHubTopics != nil || repository.ResearchTags == nil {
		t.Fatalf("new repository tag defaults=%+v", repository)
	}
	githubTags := []string{" AGENT ", "agent", "工具"}
	researchTags := []string{" 中文写作 ", "RAG"}
	observation := domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/tags", Source: domain.DiscoverySourceManual, GitHubTopics: &githubTags, ResearchTags: &researchTags}
	if _, _, err := store.UpsertRepository(ctx, observation); err != nil {
		t.Fatal(err)
	}
	githubTags[0] = "mutated-after-upsert"
	stored, err := store.GetRepository(ctx, 1)
	if err != nil || !reflect.DeepEqual(stored.GitHubTopics, []string{"agent", "工具"}) || !reflect.DeepEqual(stored.ResearchTags, []string{"中文写作", "rag"}) {
		t.Fatalf("stored tags=%+v err=%v", stored, err)
	}
	legacySubset := []string{"中文写作"}
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/tags", Source: domain.DiscoverySourceLegacy, ResearchTags: &legacySubset}); err != nil {
		t.Fatal(err)
	}
	replayed, err := store.GetRepository(ctx, 1)
	if err != nil || !reflect.DeepEqual(replayed.ResearchTags, stored.ResearchTags) {
		t.Fatal("legacy replay replaced more complete research tags")
	}
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/tags", Source: domain.DiscoverySourceGitHubSearch, Description: pointer("metadata refresh")}); err != nil {
		t.Fatal(err)
	}
	preserved, err := store.GetRepository(ctx, 1)
	if err != nil || !reflect.DeepEqual(stored.GitHubTopics, preserved.GitHubTopics) || !reflect.DeepEqual(stored.ResearchTags, preserved.ResearchTags) {
		t.Fatal("missing observation cleared tags")
	}
	empty := []string{}
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/tags", Source: domain.DiscoverySourceManual, GitHubTopics: &empty}); err != nil {
		t.Fatal(err)
	}
	knownEmpty, err := store.GetRepository(ctx, 1)
	if err != nil || knownEmpty.GitHubTopics == nil || len(knownEmpty.GitHubTopics) != 0 {
		t.Fatal("known empty topics became unknown")
	}
	var githubJSON, researchJSON string
	if err := store.db.QueryRow("SELECT github_topics_json,research_tags_json FROM repositories WHERE github_repo_id=1").Scan(&githubJSON, &researchJSON); err != nil || githubJSON != "[]" {
		t.Fatalf("raw tags=%s/%s err=%v", githubJSON, researchJSON, err)
	}
	for _, statement := range []string{`UPDATE repositories SET github_topics_json='[null]'`, `UPDATE repositories SET research_tags_json='[4]'`, `UPDATE repositories SET github_topics_json='null'`, `UPDATE repositories SET research_tags_json=NULL`} {
		if _, err := store.db.Exec(statement); err == nil {
			t.Fatalf("invalid tag JSON accepted: %s", statement)
		}
	}
	var taxonomyCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM topics").Scan(&taxonomyCount); err != nil || taxonomyCount != 0 {
		t.Fatal("raw tags polluted the classification table")
	}
}

func TestTagImportFillsOnlyUnknownMergesResearchAndInvalidatesOnlyFilledETags(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id, name := range []string{"owner/unknown", "owner/empty", "owner/known"} {
		addRepository(t, store, int64(id+1), name)
	}
	empty := []string{}
	known := []string{"existing"}
	research := []string{"中文", "rag"}
	for _, observation := range []domain.RepositoryObservation{
		{GitHubRepoID: 1, FullName: "owner/unknown", Source: domain.DiscoverySourceManual, GitHubETag: pointer("unknown-etag"), ManualNote: pointer("用户备注")},
		{GitHubRepoID: 2, FullName: "owner/empty", Source: domain.DiscoverySourceManual, GitHubTopics: &empty, GitHubETag: pointer("empty-etag")},
		{GitHubRepoID: 3, FullName: "owner/known", Source: domain.DiscoverySourceManual, GitHubTopics: &known, ResearchTags: &research, GitHubETag: pointer("known-etag")},
	} {
		if _, _, err := store.UpsertRepository(ctx, observation); err != nil {
			t.Fatal(err)
		}
	}
	putSuccess(t, store, 1, "2026-08-30", 123)
	before, err := store.GetRepository(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "原有分析", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	topic, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "operator-choice", Name: "人工分类"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveRepositoryTopic(ctx, 1, topic.ID, domain.TopicSourceManual); err != nil {
		t.Fatal(err)
	}
	assignments, err := store.ListRepositoryTopicAssignments(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	projects := []domain.RepositoryTagsImport{
		{RepositoryID: 1, FullName: "OWNER/unknown", GitHubTopics: pointer([]string{" AGENT ", "agent"}), ResearchTags: pointer([]string{"中文"})},
		{RepositoryID: 2, FullName: "owner/empty", GitHubTopics: pointer([]string{"must-not-replace"})},
		{RepositoryID: 3, FullName: "owner/known", GitHubTopics: pointer([]string{"must-not-replace"}), ResearchTags: pointer([]string{"中文", "RAG", "memory"})},
	}
	result, err := store.ImportRepositoryTags(ctx, projects)
	if err != nil || result != (domain.TagsBatchResult{Updated: 2, Skipped: 1, GitHubTopicsFilled: 1, ResearchTagsAdded: 2}) {
		t.Fatalf("tag import=%+v err=%v", result, err)
	}
	after, err := store.GetRepository(ctx, 1)
	if err != nil || after.GitHubETag != "" || !after.FirstSeenAt.Equal(before.FirstSeenAt) || after.ManualNote != before.ManualNote || !reflect.DeepEqual(after.GitHubTopics, []string{"agent"}) {
		t.Fatalf("unknown repository imported incorrectly: %+v err=%v", after, err)
	}
	for _, id := range []int64{2, 3} {
		repository, err := store.GetRepository(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if id == 2 && (repository.GitHubTopics == nil || len(repository.GitHubTopics) != 0 || repository.GitHubETag != "empty-etag") {
			t.Fatal("known empty topics/etag were overwritten")
		}
		if id == 3 && (!reflect.DeepEqual(repository.GitHubTopics, known) || repository.GitHubETag != "known-etag" || !reflect.DeepEqual(repository.ResearchTags, []string{"中文", "rag", "memory"})) {
			t.Fatal("known topics or research union changed incorrectly")
		}
	}
	current, err := store.getRepositoryAnalysis(ctx, 1)
	if err != nil || current == nil || !reflect.DeepEqual(analysis, *current) {
		t.Fatal("tag import changed analysis")
	}
	snapshot, err := store.GetDailySnapshot(ctx, 1, date("2026-08-30"))
	if err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 123 {
		t.Fatal("tag import changed stars")
	}
	currentAssignments, err := store.ListRepositoryTopicAssignments(ctx, nil)
	if err != nil || !reflect.DeepEqual(assignments, currentAssignments) {
		t.Fatal("tag import changed manual classifications/vetoes")
	}
	store.now = func() time.Time { return testNow.Add(time.Hour) }
	repeat, err := store.ImportRepositoryTags(ctx, projects)
	if err != nil || repeat != (domain.TagsBatchResult{Skipped: 3}) {
		t.Fatalf("repeat=%+v err=%v", repeat, err)
	}
	repeated, err := store.GetRepository(ctx, 1)
	if err != nil || !repeated.UpdatedAt.Equal(after.UpdatedAt) {
		t.Fatal("idempotent import rewrote timestamps")
	}
}

func TestTagImportIdentityFailureRollsBackTagsAndETag(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/one")
	addRepository(t, store, 2, "owner/two")
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 1, FullName: "owner/one", Source: domain.DiscoverySourceManual, GitHubETag: pointer("keep-me")}); err != nil {
		t.Fatal(err)
	}
	for _, second := range []domain.RepositoryTagsImport{
		{RepositoryID: 2, FullName: "wrong/name", ResearchTags: pointer([]string{"bad"})},
		{RepositoryID: 1, FullName: "owner/one", ResearchTags: pointer([]string{"duplicate"})},
		{RepositoryID: 2, FullName: "owner/two", ResearchTags: pointer([]string{"invalid\nlabel"})},
	} {
		result, err := store.ImportRepositoryTags(ctx, []domain.RepositoryTagsImport{{RepositoryID: 1, FullName: "owner/one", GitHubTopics: pointer([]string{})}, second})
		if err == nil || result != (domain.TagsBatchResult{}) {
			t.Fatal("invalid tag batch reported success")
		}
		repository, err := store.GetRepository(ctx, 1)
		if err != nil || repository.GitHubTopics != nil || repository.GitHubETag != "keep-me" {
			t.Fatal("rollback left partial tags/ETag invalidation")
		}
	}
	result, err := store.ImportRepositoryTags(ctx, []domain.RepositoryTagsImport{{RepositoryID: 1, FullName: "owner/one", GitHubTopics: pointer([]string{})}})
	if err != nil || result.GitHubTopicsFilled != 1 {
		t.Fatal("known empty tags did not fill unknown state")
	}
	repository, _ := store.GetRepository(ctx, 1)
	if repository.GitHubTopics == nil || repository.GitHubETag != "" {
		t.Fatal("empty tag bootstrap did not invalidate ETag")
	}
}

func TestRepositoryTagsMigrationFromV3AndV4PreservesHistory(t *testing.T) {
	for _, version := range []int{3, 4} {
		t.Run(string(rune('0'+version)), func(t *testing.T) {
			db, _ := newV3MigrationFixture(t)
			if _, err := db.Exec("DELETE FROM repository_topics WHERE repository_id>=10155"); err != nil {
				t.Fatal(err)
			}
			if version == 4 {
				script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
				if err != nil {
					t.Fatal(err)
				}
				if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)); err != nil {
					t.Fatal(err)
				}
			}
			before := migrationEvidence(t, db)
			if len(before["foreign_key_check"]) != 155 {
				t.Fatal("fixture does not match historical orphan count")
			}
			if err := applyMigrations(context.Background(), db); err != nil {
				t.Fatal(err)
			}
			assertMigrationSettings(t, db, 5, 0)
			if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
				t.Fatal("metadata migration changed existing history/schema/custom data")
			}
			var githubJSON sql.NullString
			var researchJSON string
			if err := db.QueryRow("SELECT github_topics_json,research_tags_json FROM repositories WHERE github_repo_id=42").Scan(&githubJSON, &researchJSON); err != nil || githubJSON.Valid || researchJSON != "[]" {
				t.Fatal("migration fabricated raw GitHub topics or research tags")
			}
			if err := applyMigrations(context.Background(), db); err != nil {
				t.Fatal(err)
			}
		})
	}
}

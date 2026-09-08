package sqlite

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func seedQueryTagRepositories(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	for _, input := range []struct {
		id               int64
		name             string
		github, research []string
		future           bool
	}{
		{1, "owner/one", []string{" Agent ", "SKILLS", "unknown-upstream-label", "　ÖKO　"}, []string{"投资", "skills"}, false},
		{2, "owner/two", []string{"skills", "agentic"}, []string{"投资"}, false},
		{3, "owner/taxonomy-only", nil, []string{}, false},
		{4, "owner/future", []string{"future-label"}, []string{"未来"}, true},
		{5, "owner/no-tags", []string{}, []string{}, false},
	} {
		firstSeen := testNow.AddDate(0, 0, -30)
		if input.future {
			firstSeen = testNow.AddDate(0, 0, 1)
		}
		_, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
			GitHubRepoID: input.id, FullName: input.name, Source: domain.DiscoverySourceGitHubSearch,
			DiscoveredAt: firstSeen, GitHubTopics: &input.github, ResearchTags: &input.research,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	putSuccess(t, store, 1, "2026-08-23", 80)
	putSuccess(t, store, 1, "2026-08-30", 100)
	putSuccess(t, store, 2, "2026-08-23", 180)
	putSuccess(t, store, 2, "2026-08-30", 200)
	putSuccess(t, store, 3, "2026-08-23", 9000)
	putSuccess(t, store, 3, "2026-08-30", 10000)
	category, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "research-agents", Name: "Research Agents", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: 3, TopicID: category.ID, Source: domain.TopicSourceManual}); err != nil {
		t.Fatal(err)
	}
	skills, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "skills", Name: "Skills", Status: domain.TopicActive})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: 1, TopicID: skills.ID, Source: domain.TopicSourceAuto}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RemoveRepositoryTopic(ctx, 3, skills.ID, domain.TopicSourceManual); err != nil {
		t.Fatal(err)
	}
	archived, _, err := store.UpsertTopic(ctx, domain.Topic{Slug: "retired-category", Name: "Retired", Status: domain.TopicArchived})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{RepositoryID: 3, TopicID: archived.ID, Source: domain.TopicSourceManual}); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryTagsUnionKeepsUnknownAndChineseLabelsWithDistinctProjectCounts(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	seedQueryTagRepositories(t, store)
	tags, err := store.ListRepositoryTags(ctx, date("2026-08-30"))
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]int{}
	for _, tag := range tags {
		actual[tag.Name] = tag.Count
	}
	want := map[string]int{"agent": 1, "skills": 2, "unknown-upstream-label": 1, "öko": 1, "投资": 2, "agentic": 1, "research-agents": 1, "research agents": 1}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("label union/counts = %+v, want %+v", actual, want)
	}
	if tags[0].Count != 2 || tags[1].Count != 2 {
		t.Fatal("label choices should prioritize the most represented labels")
	}
	future, err := store.ListRepositoryTags(ctx, date("2026-08-31"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, tag := range future {
		found = found || tag.Name == "future-label"
	}
	if !found {
		t.Fatal("label missing after its project's first-seen date")
	}
	if _, err := store.GetTopicBySlug(ctx, "unknown-upstream-label"); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatal("a raw label polluted the taxonomy")
	}
	if _, err := store.ListRepositoryTags(ctx, domain.Date("not-a-date")); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("invalid date error = %v", err)
	}
}

func TestExactRepositoryTagFilterRunsBeforeRankingSearchAndPagination(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	seedQueryTagRepositories(t, store)
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), WindowDays: 7, Tag: " \nSKILLS\t ", Sort: domain.RepositoryTrendSortStars, Limit: 1}
	first, err := store.ListRepositoryTrends(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	assertRadarIDs(t, first.Items, []int64{2})
	if first.Total != 2 || first.Coverage.ScopeCount != 2 || first.Coverage.ComparableCount != 2 || !first.HasMore || first.Items[0].CurrentRank == nil || *first.Items[0].CurrentRank != 1 {
		t.Fatalf("tag scope did not precede ranks and paging: %+v", first)
	}
	query.AfterID = pointer(int64(2))
	next, err := store.ListRepositoryTrends(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	assertRadarIDs(t, next.Items, []int64{1})
	if next.HasMore || next.Items[0].CurrentRank == nil || *next.Items[0].CurrentRank != 2 {
		t.Fatal("second page lost the scoped sample rank")
	}
	query.AfterID, query.Search, query.Limit = nil, "owner/one", 20
	searched, err := store.ListRepositoryTrends(ctx, query)
	if err != nil || len(searched.Items) != 1 || searched.Coverage.ScopeCount != 2 || *searched.Items[0].CurrentRank != 2 {
		t.Fatalf("search recalculated rank inside only its result: %+v, %v", searched, err)
	}
	for _, tag := range []string{"skill", "agen", "skills' OR 1=1 --", "retired-category"} {
		page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: query.AsOf, Tag: tag})
		if err != nil || page.Total != 0 {
			t.Fatalf("tag %q was not matched literally: %+v, %v", tag, page, err)
		}
	}
	for _, test := range []struct {
		tag string
		ids []int64
	}{
		{"　投资　", []int64{2, 1}}, {"AGENT", []int64{1}}, {"ÖKO", []int64{1}},
		{" Research Agents ", []int64{3}}, {"research-agents", []int64{3}},
	} {
		page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: query.AsOf, Tag: test.tag, Sort: domain.RepositoryTrendSortStars})
		if err != nil {
			t.Fatal(err)
		}
		assertRadarIDs(t, page.Items, test.ids)
	}
	combined, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: query.AsOf, Tag: "skills", TopicSlug: "research-agents"})
	if err != nil || combined.Total != 0 {
		t.Fatal("raw tag and taxonomy filters must both apply")
	}
	all, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: query.AsOf})
	if err != nil || all.Total != 4 {
		t.Fatalf("unlabeled or unknown-tag repositories disappeared without a filter: %+v, %v", all, err)
	}
}

func TestRepositoryTagOptionsAreEmptyBeforeTheFirstProject(t *testing.T) {
	store, _ := newTestStore(t)
	tags, err := store.ListRepositoryTags(context.Background(), date("2026-08-30"))
	if err != nil || tags == nil || len(tags) != 0 {
		t.Fatalf("empty tag options = %+v, %v", tags, err)
	}
}

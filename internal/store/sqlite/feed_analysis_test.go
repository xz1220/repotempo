package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/xz1220/repotempo/internal/domain"
	modernsqlite "modernc.org/sqlite"
)

func TestTrendFeedLoadsOnlyItsPageAnalysesAndPreservesProvenance(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 4; id++ {
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/feed-%d", id), domain.DiscoverySourceGitHubSearch, testNow.AddDate(0, 0, -30))
		putSuccess(t, store, id, "2026-08-30", 1000-id*10)
	}
	first, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 1, SummaryZH: "用于长期跟踪项目增长。", Source: "codex", Model: "fixture-model", AnalyzedAt: testNow, KeyPoints: []string{"保存真实快照"}, UseCases: []string{"开源研究"}, TechnicalNotes: "原始技术备注"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{3, 4} {
		if _, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: id, SummaryZH: fmt.Sprintf("解读 %d", id), Source: "manual", Model: "", AnalyzedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	// An old incomplete analysis stays incomplete. An invalid off-page record
	// must not be decoded while loading this page's three project IDs.
	if _, err := store.db.Exec("UPDATE repository_analyses SET summary_zh = '' WHERE repository_id = 3; UPDATE repository_analyses SET analyzed_at = 'invalid-off-page-date' WHERE repository_id = 4"); err != nil {
		t.Fatal(err)
	}
	note := "仅有关注备注，不能假装生成了解读"
	if _, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: 2, FullName: "owner/feed-2", Source: domain.DiscoverySourceManual, ManualNote: &note}); err != nil {
		t.Fatal(err)
	}
	page, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), Sort: domain.RepositoryTrendSortStars, Limit: 3})
	if err != nil || !page.HasMore || len(page.Items) != 3 {
		t.Fatalf("feed query: %+v, %v", page, err)
	}
	assertRadarIDs(t, page.Items, []int64{1, 2, 3})
	if page.Items[0].Analysis == nil || !reflect.DeepEqual(*page.Items[0].Analysis, first) {
		t.Fatalf("stored analysis or provenance changed: %+v", page.Items[0].Analysis)
	}
	if page.Items[1].Analysis != nil || page.Items[1].Repository.ManualNote != note {
		t.Fatal("description or manual note was substituted for an absent analysis")
	}
	if page.Items[2].Analysis == nil || page.Items[2].Analysis.SummaryZH != "" || page.Items[2].Analysis.Source != "manual" {
		t.Fatal("an incomplete saved summary was synthesized or lost")
	}
	if page.Items[0].Analysis == page.Items[2].Analysis {
		t.Fatal("distinct analyses share a mutable pointer")
	}
	empty, err := store.ListRepositoryTrends(ctx, domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), Search: "nothing-matches"})
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("empty feed read unrelated analyses: %+v, %v", empty, err)
	}
	detail, err := store.GetRepositoryDetail(ctx, 1, date("2026-08-30"))
	if err != nil || !reflect.DeepEqual(detail.Analysis, detail.Metric.Analysis) || !reflect.DeepEqual(detail.Analysis, page.Items[0].Analysis) {
		t.Fatalf("feed/detail analysis diverged: %+v, %v", detail, err)
	}
}

var feedDriverSequence atomic.Int64

type feedQueryCounter struct {
	driver.Driver
	analyses atomic.Int64
}

func (counter *feedQueryCounter) Open(name string) (driver.Conn, error) {
	connection, err := counter.Driver.Open(name)
	if err != nil {
		return nil, err
	}
	return &feedCountingConnection{Conn: connection, counter: counter}, nil
}

type feedCountingConnection struct {
	driver.Conn
	counter *feedQueryCounter
}

func (connection *feedCountingConnection) QueryContext(ctx context.Context, statement string, args []driver.NamedValue) (driver.Rows, error) {
	if strings.Contains(strings.ToLower(statement), "from repository_analyses") {
		connection.counter.analyses.Add(1)
	}
	return connection.Conn.(driver.QueryerContext).QueryContext(ctx, statement, args)
}

func TestTrendFeedUsesOneAnalysisQueryForEachFiftyRepositoryPage(t *testing.T) {
	store, path := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 55; id++ {
		addRepositoryFromSource(t, store, id, fmt.Sprintf("owner/batch-%d", id), domain.DiscoverySourceGitHubSearch, testNow.AddDate(0, 0, -30))
		putSuccess(t, store, id, "2026-08-30", 1000-id)
		if _, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: id, SummaryZH: fmt.Sprintf("项目 %d 的解读", id), Source: "codex", AnalyzedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.db.Close(); err != nil {
		t.Fatal(err)
	}
	counter := &feedQueryCounter{Driver: &modernsqlite.Driver{}}
	driverName := fmt.Sprintf("feed-query-counter-%d", feedDriverSequence.Add(1))
	sql.Register(driverName, counter)
	database, err := sql.Open(driverName, path)
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	store.db = database
	query := domain.RepositoryTrendQuery{AsOf: date("2026-08-30"), Sort: domain.RepositoryTrendSortStars, Limit: 50}
	first, err := store.ListRepositoryTrends(ctx, query)
	if err != nil || len(first.Items) != 50 || !first.HasMore {
		t.Fatalf("first page: %d items, %v", len(first.Items), err)
	}
	if got := counter.analyses.Load(); got != 1 {
		t.Fatalf("50 project analyses used %d queries, want one batch query", got)
	}
	query.AfterID = pointer(first.Items[len(first.Items)-1].Repository.GitHubRepoID)
	next, err := store.ListRepositoryTrends(ctx, query)
	if err != nil || len(next.Items) != 5 || next.HasMore {
		t.Fatalf("next page: %d items, %v", len(next.Items), err)
	}
	if got := counter.analyses.Load(); got != 2 {
		t.Fatalf("two pages used %d analysis queries, want two", got)
	}
	for _, page := range []domain.RepositoryTrendPage{first, next} {
		for _, entry := range page.Items {
			if entry.Analysis == nil || entry.Analysis.RepositoryID != entry.Repository.GitHubRepoID || entry.Analysis.SummaryZH != fmt.Sprintf("项目 %d 的解读", entry.Repository.GitHubRepoID) {
				t.Fatalf("analysis attached to wrong project: %+v", entry)
			}
		}
	}
}

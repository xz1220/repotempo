package sqlite

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/domain"
	corestore "github.com/xz1220/github-radar/internal/store"
)

var testNow = time.Date(2026, 8, 30, 4, 0, 0, 0, time.UTC)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "github-radar.db")
	store, err := OpenWithConfig(context.Background(), Config{
		Path: path,
		Now:  func() time.Time { return testNow },
	})
	if err != nil {
		t.Fatalf("open test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close test store: %v", err)
		}
	})
	return store, path
}

func pointer[T any](value T) *T {
	return &value
}

func date(value string) domain.Date {
	parsed, err := domain.ParseDate(value)
	if err != nil {
		panic(err)
	}
	return parsed
}

func addRepository(t *testing.T, store *Store, id int64, fullName string) domain.Repository {
	t.Helper()
	description := "description for " + fullName
	repository, _, err := store.UpsertRepository(context.Background(), domain.RepositoryObservation{
		GitHubRepoID: id,
		FullName:     fullName,
		HTMLURL:      "https://github.com/" + fullName,
		Description:  &description,
		Source:       domain.DiscoverySourceGitHubSearch,
		Profile:      "test-profile",
		DiscoveredAt: testNow.Add(-time.Hour),
	})
	if err != nil {
		t.Fatalf("add repository %s: %v", fullName, err)
	}
	return repository
}

func putSuccess(t *testing.T, store *Store, repositoryID int64, snapshotDate string, stars int64) {
	t.Helper()
	_, err := store.PutDailySnapshot(context.Background(), domain.DailySnapshot{
		RepositoryID: repositoryID,
		SnapshotDate: date(snapshotDate),
		CapturedAt:   testNow,
		StarCount:    &stars,
		FetchStatus:  domain.FetchSuccess,
		HTTPStatus:   pointer(200),
	})
	if err != nil {
		t.Fatalf("put successful snapshot for %d on %s: %v", repositoryID, snapshotDate, err)
	}
}

func putFailure(t *testing.T, store *Store, repositoryID int64, snapshotDate string) {
	t.Helper()
	_, err := store.PutDailySnapshot(context.Background(), domain.DailySnapshot{
		RepositoryID: repositoryID,
		SnapshotDate: date(snapshotDate),
		CapturedAt:   testNow,
		FetchStatus:  domain.FetchFailed,
		HTTPStatus:   pointer(503),
		ErrorCode:    "upstream_unavailable",
	})
	if err != nil {
		t.Fatalf("put failed snapshot for %d on %s: %v", repositoryID, snapshotDate, err)
	}
}

func TestMigrationIsIdempotentAndCreatesExactlyFiveTables(t *testing.T) {
	store, path := newTestStore(t)
	addRepository(t, store, 1, "openai/codex")

	rows, err := store.db.Query(`
SELECT name FROM sqlite_schema
WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
ORDER BY name`)
	if err != nil {
		t.Fatalf("list tables: %v", err)
	}
	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan table: %v", err)
		}
		tables = append(tables, name)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("close table rows: %v", err)
	}
	wantTables := []string{"daily_snapshots", "job_runs", "repositories", "repository_topics", "topics"}
	if !reflect.DeepEqual(tables, wantTables) {
		t.Fatalf("tables = %v, want %v", tables, wantTables)
	}
	var journalMode string
	if err := store.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("read journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	var foreignKeys int
	if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
	var migrationVersion int
	if err := store.db.QueryRow("PRAGMA user_version").Scan(&migrationVersion); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if migrationVersion != 1 {
		t.Fatalf("user_version = %d, want 1", migrationVersion)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("close before reopen: %v", err)
	}
	reopened, err := OpenWithConfig(context.Background(), Config{Path: path, Now: func() time.Time { return testNow }})
	if err != nil {
		t.Fatalf("reopen migrated database: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if _, err := reopened.GetRepository(context.Background(), 1); err != nil {
		t.Fatalf("data did not survive repeated migration: %v", err)
	}
	if err := reopened.db.QueryRow("PRAGMA user_version").Scan(&migrationVersion); err != nil || migrationVersion != 1 {
		t.Fatalf("reopened user_version = %d, err = %v", migrationVersion, err)
	}
}

func TestRepositoryUpsertUsesIDAndPreservesRenameHistory(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	description := "new description"
	first, created, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 42,
		FullName:     "old-owner/project",
		Source:       domain.DiscoverySourceGitHubSearch,
		Profile:      "popular",
		DiscoveredAt: testNow.Add(-24 * time.Hour),
	})
	if err != nil || !created {
		t.Fatalf("first upsert = (%+v, %t, %v), want created", first, created, err)
	}
	updated, created, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 42,
		FullName:     "old-owner/project",
		Description:  &description,
		Source:       domain.DiscoverySourceOSSInsight,
		DiscoveredAt: testNow.Add(-48 * time.Hour),
	})
	if err != nil || created {
		t.Fatalf("rename upsert = (%+v, %t, %v), want update", updated, created, err)
	}
	if updated.FirstSeenSource != domain.DiscoverySourceOSSInsight || !updated.FirstSeenAt.Equal(testNow.Add(-48*time.Hour)) {
		t.Fatalf("earliest observation not preserved: %+v", updated)
	}
	if !reflect.DeepEqual(updated.DiscoverySources, []domain.DiscoverySource{
		domain.DiscoverySourceGitHubSearch,
		domain.DiscoverySourceOSSInsight,
	}) {
		t.Fatalf("discovery sources = %v", updated.DiscoverySources)
	}
	updated, created, err = store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 42,
		FullName:     "new-owner/project",
		HTMLURL:      "https://github.com/new-owner/project",
		Source:       domain.DiscoverySourceManual,
		DiscoveredAt: testNow,
	})
	if err != nil || created || updated.FullName != "new-owner/project" {
		t.Fatalf("rename upsert = (%+v, %t, %v), want current new name", updated, created, err)
	}
	if !reflect.DeepEqual(updated.PreviousNames, []string{"old-owner/project"}) {
		t.Fatalf("previous names = %v", updated.PreviousNames)
	}
	// An out-of-order historical import records its old name without rolling
	// the current GitHub name backward.
	updated, _, err = store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 42,
		FullName:     "ancient-owner/project",
		Source:       domain.DiscoverySourceLegacy,
		DiscoveredAt: testNow.Add(-96 * time.Hour),
	})
	if err != nil || updated.FullName != "new-owner/project" {
		t.Fatalf("historical-name upsert = (%+v, %v)", updated, err)
	}
	if !reflect.DeepEqual(updated.PreviousNames, []string{"old-owner/project", "ancient-owner/project"}) {
		t.Fatalf("previous names after import = %v", updated.PreviousNames)
	}
	repositories, err := store.ListRepositories(ctx, domain.RepositoryFilter{})
	if err != nil {
		t.Fatalf("list repositories: %v", err)
	}
	if len(repositories) != 1 || repositories[0].GitHubRepoID != 42 {
		t.Fatalf("repositories = %+v, want one ID 42", repositories)
	}
	_, _, err = store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID: 99,
		FullName:     "new-owner/project",
		Source:       domain.DiscoverySourceManual,
	})
	if !errors.Is(err, corestore.ErrRepositoryIdentityMismatch) {
		t.Fatalf("conflicting identity error = %v", err)
	}
}

func TestAutomatedRediscoveryDoesNotEraseManualRegistryState(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	focus := true
	note := "keep this note"
	_, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID:     7,
		FullName:         "owner/focus",
		Source:           domain.DiscoverySourceManual,
		DiscoveredAt:     testNow.Add(-time.Hour),
		MonitoringStatus: domain.MonitoringPaused,
		IsFocus:          &focus,
		ManualNote:       &note,
	})
	if err != nil {
		t.Fatalf("add manual repository: %v", err)
	}
	notFocus := false
	emptyNote := ""
	rediscovered, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID:     7,
		FullName:         "owner/focus",
		Source:           domain.DiscoverySourceGitHubSearch,
		DiscoveredAt:     testNow,
		MonitoringStatus: domain.MonitoringActive,
		IsFocus:          &notFocus,
		ManualNote:       &emptyNote,
	})
	if err != nil {
		t.Fatalf("rediscover repository: %v", err)
	}
	if rediscovered.MonitoringStatus != domain.MonitoringPaused || !rediscovered.IsFocus || rediscovered.ManualNote != note {
		t.Fatalf("automated rediscovery erased manual state: %+v", rediscovered)
	}
	configNote := "configuration default"
	replayed, _, err := store.UpsertRepository(ctx, domain.RepositoryObservation{
		GitHubRepoID:     7,
		FullName:         "owner/focus",
		Source:           domain.DiscoverySourceManual,
		Profile:          "config-watchlist",
		DiscoveredAt:     testNow.Add(time.Minute),
		MonitoringStatus: domain.MonitoringActive,
		IsFocus:          &notFocus,
		ManualNote:       &configNote,
	})
	if err != nil {
		t.Fatalf("replay configured watchlist: %v", err)
	}
	if replayed.MonitoringStatus != domain.MonitoringPaused || !replayed.IsFocus || replayed.ManualNote != note {
		t.Fatalf("watchlist replay erased explicit state: %+v", replayed)
	}
	if err := store.SetRepositoryMonitoringStatus(ctx, 7, domain.MonitoringActive); err != nil {
		t.Fatalf("explicitly resume repository: %v", err)
	}
	resumed, err := store.GetRepository(ctx, 7)
	if err != nil || resumed.MonitoringStatus != domain.MonitoringActive {
		t.Fatalf("resumed repository = (%+v, %v)", resumed, err)
	}
}

func TestSnapshotWriteProtectsSuccessAndRepairsFailure(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 1, "owner/project")

	stars100 := int64(100)
	first, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID: 1,
		SnapshotDate: date("2026-08-29"),
		CapturedAt:   testNow.Add(-24 * time.Hour),
		StarCount:    &stars100,
		FetchStatus:  domain.FetchSuccess,
		HTTPStatus:   pointer(200),
	})
	if err != nil || first.Disposition != domain.SnapshotInserted {
		t.Fatalf("first snapshot = (%+v, %v)", first, err)
	}
	stars999 := int64(999)
	protected, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID: 1,
		SnapshotDate: date("2026-08-29"),
		CapturedAt:   testNow,
		StarCount:    &stars999,
		FetchStatus:  domain.FetchSuccess,
		HTTPStatus:   pointer(200),
	})
	if err != nil || protected.Disposition != domain.SnapshotSuccessProtected || *protected.Snapshot.StarCount != 100 {
		t.Fatalf("protected snapshot = (%+v, %v)", protected, err)
	}

	failed, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID:   1,
		SnapshotDate:   date("2026-08-30"),
		FetchStatus:    domain.FetchFailed,
		HTTPStatus:     pointer(503),
		ErrorCode:      "unreachable",
		OSSTodayRank:   pointer(3),
		OSSWindowStars: pointer(int64(25)),
	})
	if err != nil || failed.Disposition != domain.SnapshotInserted || failed.Snapshot.StarCount != nil {
		t.Fatalf("failed snapshot = (%+v, %v)", failed, err)
	}
	invalidZero := int64(0)
	_, err = store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID: 1,
		SnapshotDate: date("2026-08-31"),
		StarCount:    &invalidZero,
		FetchStatus:  domain.FetchFailed,
	})
	if !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("failed snapshot with zero star error = %v", err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO daily_snapshots
    (repository_id, snapshot_date, captured_at, star_count, fetch_status, created_at)
VALUES (?, ?, ?, ?, ?, ?)`, 1, "2026-09-01", storedTime(testNow), 0, domain.FetchFailed, storedTime(testNow)); err == nil {
		t.Fatal("database accepted a failed snapshot with star_count=0")
	}
	stars110 := int64(110)
	repaired, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID: 1,
		SnapshotDate: date("2026-08-30"),
		CapturedAt:   testNow,
		StarCount:    &stars110,
		FetchStatus:  domain.FetchSuccess,
		HTTPStatus:   pointer(200),
	})
	if err != nil || repaired.Disposition != domain.SnapshotFailureRepaired {
		t.Fatalf("repaired snapshot = (%+v, %v)", repaired, err)
	}
	if repaired.Snapshot.OSSTodayRank == nil || *repaired.Snapshot.OSSTodayRank != 3 {
		t.Fatalf("repair lost OSS metadata: %+v", repaired.Snapshot)
	}
	stored, err := store.GetDailySnapshot(ctx, 1, date("2026-08-30"))
	if err != nil || stored.StarCount == nil || *stored.StarCount != 110 || stored.FetchStatus != domain.FetchSuccess {
		t.Fatalf("stored repaired snapshot = (%+v, %v)", stored, err)
	}

	stars304 := int64(110)
	if _, err := store.PutDailySnapshot(ctx, domain.DailySnapshot{
		RepositoryID: 1,
		SnapshotDate: date("2026-08-31"),
		StarCount:    &stars304,
		FetchStatus:  domain.FetchSuccess,
		HTTPStatus:   pointer(304),
	}); err != nil {
		t.Fatalf("store 304 proven unchanged snapshot: %v", err)
	}
	latest, err := store.GetLatestSuccessfulSnapshot(ctx, 1, date("2026-08-31"))
	if err != nil || latest.SnapshotDate != date("2026-08-31") || *latest.StarCount != 110 {
		t.Fatalf("latest successful snapshot = (%+v, %v)", latest, err)
	}

	var rowCount int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM daily_snapshots WHERE repository_id = 1").Scan(&rowCount); err != nil {
		t.Fatalf("count snapshots: %v", err)
	}
	if rowCount != 3 {
		t.Fatalf("snapshot rows = %d, want 3", rowCount)
	}
}

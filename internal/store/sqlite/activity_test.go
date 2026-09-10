package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
	"github.com/xz1220/repotempo/migrations"
)

func activityFixture(repositoryID int64) domain.RepositoryActivity {
	end := time.Date(2026, 9, 8, 13, 45, 0, 123456789, time.UTC)
	start := time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC)
	activity := domain.RepositoryActivity{
		RepositoryID: repositoryID, DefaultBranch: "main", HeadSHA: strings.Repeat("ab", 20), IsFork: true,
		WindowStart: start, WindowEnd: end, FetchedAt: end.Add(time.Minute),
		LatestCommitAt: pointer(end.Add(-time.Hour)), Commits: 3, ActiveDays: 2, Complete: true,
		Daily: make([]domain.ActivityDay, 30),
	}
	for index := range activity.Daily {
		activity.Daily[index].Date = start.AddDate(0, 0, index).Format(time.DateOnly)
	}
	activity.Daily[0].Count, activity.Daily[29].Count = 2, 1
	return activity
}

func TestActivityRoundTripIsIndependentOfRepositoryMetadataAndReadings(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	before := addRepository(t, store, 42, "owner/activity")
	if before.Activity != nil {
		t.Fatal("unfetched activity must remain unknown")
	}
	analysis, err := store.PutRepositoryAnalysis(ctx, domain.RepositoryAnalysis{RepositoryID: 42, SummaryZH: "原有中文解读", Source: "manual"})
	if err != nil {
		t.Fatal(err)
	}
	putSuccess(t, store, 42, "2026-08-30", 123)
	activity := activityFixture(42)
	if err := store.PutRepositoryActivity(ctx, 42, activity); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetRepository(ctx, 42)
	if err != nil || stored.Activity == nil || !reflect.DeepEqual(*stored.Activity, activity) {
		t.Fatalf("activity roundtrip=%+v err=%v", stored.Activity, err)
	}
	stored.Activity = nil
	if !reflect.DeepEqual(stored, before) {
		t.Fatal("activity changed unrelated repository fields or updated_at")
	}
	currentAnalysis, err := store.getRepositoryAnalysis(ctx, 42)
	if err != nil || !reflect.DeepEqual(currentAnalysis, &analysis) {
		t.Fatal("activity changed the saved project reading")
	}
	if snapshot, err := store.GetDailySnapshot(ctx, 42, date("2026-08-30")); err != nil || snapshot.StarCount == nil || *snapshot.StarCount != 123 {
		t.Fatal("activity changed Star history")
	}
	activity.Daily[0].Count = 499
	*activity.LatestCommitAt = activity.WindowStart
	tags := []string{"agent", "投资"}
	metadata := domain.RepositoryObservation{
		GitHubRepoID: 42, FullName: "new-owner/activity", Source: domain.DiscoverySourceGitHubSearch,
		DiscoveredAt: testNow.Add(time.Hour), Description: pointer("updated metadata"), GitHubTopics: &tags,
	}
	updated, _, err := store.UpsertRepository(ctx, metadata)
	if err != nil || updated.Activity == nil || !reflect.DeepEqual(*updated.Activity, activityFixture(42)) {
		t.Fatalf("metadata update cleared or mutated independent cache: %+v err=%v", updated.Activity, err)
	}
	if err := store.SetRepositoryGitHubStatus(ctx, 42, domain.GitHubArchived); err != nil {
		t.Fatal(err)
	}
	updated, err = store.GetRepositoryByFullName(ctx, "new-owner/activity")
	if err != nil || updated.Activity == nil || updated.Activity.Commits != 3 {
		t.Fatal("status update cleared activity")
	}
	var raw string
	if err := store.db.QueryRow("SELECT activity_json FROM repositories WHERE github_repo_id=42").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil || object["repository_id"] != float64(42) || object["window_start"] == nil || object["RepositoryID"] != nil {
		t.Fatalf("activity JSON contract changed: %s", raw)
	}
}

func TestActivityUnknownEmptyAndTruncatedEvidenceRemainDistinct(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for id := int64(1); id <= 4; id++ {
		addRepository(t, store, id, fmt.Sprintf("owner/activity-%d", id))
	}
	empty := activityFixture(2)
	empty.Commits, empty.ActiveDays, empty.LatestCommitAt, empty.HeadSHA = 0, 0, nil, ""
	for index := range empty.Daily {
		empty.Daily[index].Count = 0
	}
	if err := store.PutRepositoryActivity(ctx, 2, empty); err != nil {
		t.Fatal(err)
	}
	inactive := empty
	inactive.RepositoryID, inactive.HeadSHA = 3, strings.Repeat("0a", 20)
	inactive.LatestCommitAt = pointer(inactive.WindowStart.Add(-time.Hour))
	if err := store.PutRepositoryActivity(ctx, 3, inactive); err != nil {
		t.Fatal(err)
	}
	partial := activityFixture(4)
	partial.Complete = false
	if err := store.PutRepositoryActivity(ctx, 4, partial); err != nil {
		t.Fatal("a shorter page budget or deduplicated partial result must be accepted:", err)
	}
	for id := int64(1); id <= 4; id++ {
		repository, err := store.GetRepository(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if (id == 1) != (repository.Activity == nil) {
			t.Fatalf("unknown and observed-zero activity conflated for %d", id)
		}
		if id == 4 && repository.Activity.Complete {
			t.Fatal("truncation flag lost")
		}
	}
}

func TestActivityRejectsInconsistentEvidenceWithoutWriting(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 42, "owner/activity")
	for name, change := range map[string]func(*domain.RepositoryActivity){
		"mismatched ID":      func(a *domain.RepositoryActivity) { a.RepositoryID = 43 },
		"missing branch":     func(a *domain.RepositoryActivity) { a.DefaultBranch = "" },
		"unsafe branch":      func(a *domain.RepositoryActivity) { a.DefaultBranch = "main\n" },
		"abbreviated SHA":    func(a *domain.RepositoryActivity) { a.HeadSHA = "abcd" },
		"bad SHA":            func(a *domain.RepositoryActivity) { a.HeadSHA = strings.Repeat("z", 40) },
		"missing SHA":        func(a *domain.RepositoryActivity) { a.HeadSHA = "" },
		"missing end":        func(a *domain.RepositoryActivity) { a.WindowEnd = time.Time{} },
		"missing fetch":      func(a *domain.RepositoryActivity) { a.FetchedAt = time.Time{} },
		"wrong window date":  func(a *domain.RepositoryActivity) { a.WindowStart = a.WindowStart.AddDate(0, 0, 1) },
		"nonmidnight start":  func(a *domain.RepositoryActivity) { a.WindowStart = a.WindowStart.Add(time.Nanosecond) },
		"fetch before end":   func(a *domain.RepositoryActivity) { a.FetchedAt = a.WindowEnd.Add(-time.Nanosecond) },
		"over cap":           func(a *domain.RepositoryActivity) { a.Commits = 501 },
		"negative total":     func(a *domain.RepositoryActivity) { a.Commits = -1 },
		"bad active days":    func(a *domain.RepositoryActivity) { a.ActiveDays = 31 },
		"total mismatch":     func(a *domain.RepositoryActivity) { a.Commits++ },
		"active mismatch":    func(a *domain.RepositoryActivity) { a.ActiveDays++ },
		"missing days":       func(a *domain.RepositoryActivity) { a.Daily = a.Daily[:29] },
		"duplicate date":     func(a *domain.RepositoryActivity) { a.Daily[1].Date = a.Daily[0].Date },
		"out of window date": func(a *domain.RepositoryActivity) { a.Daily[0].Date = "2026-08-09" },
		"negative daily":     func(a *domain.RepositoryActivity) { a.Daily[0].Count = -1 },
		"missing latest":     func(a *domain.RepositoryActivity) { a.LatestCommitAt = nil },
		"future latest":      func(a *domain.RepositoryActivity) { a.LatestCommitAt = pointer(a.WindowEnd.Add(time.Nanosecond)) },
		"zero latest":        func(a *domain.RepositoryActivity) { a.LatestCommitAt = pointer(time.Time{}) },
	} {
		t.Run(name, func(t *testing.T) {
			activity := activityFixture(42)
			change(&activity)
			if err := store.PutRepositoryActivity(ctx, 42, activity); !errors.Is(err, corestore.ErrInvalid) {
				t.Fatalf("invalid evidence error = %v", err)
			}
			repository, err := store.GetRepository(ctx, 42)
			if err != nil || repository.Activity != nil {
				t.Fatal("invalid evidence left a cache value")
			}
		})
	}
	if err := store.PutRepositoryActivity(ctx, 404, activityFixture(404)); !errors.Is(err, corestore.ErrNotFound) {
		t.Fatalf("unknown repository write = %v", err)
	}
	if err := store.PutRepositoryActivity(ctx, 0, activityFixture(0)); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatalf("zero ID write = %v", err)
	}
}

func TestActivityNewerFetchWinsWithoutTimestampStringOrderingBugs(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 42, "owner/activity")
	activity := activityFixture(42)
	activity.FetchedAt = activity.FetchedAt.Truncate(time.Second).Add(time.Nanosecond)
	if err := store.PutRepositoryActivity(ctx, 42, activity); err != nil {
		t.Fatal(err)
	}
	for _, delta := range []time.Duration{-time.Nanosecond, 0} {
		older := activity
		older.DefaultBranch = "must-not-replace"
		older.FetchedAt = older.FetchedAt.Add(delta).In(time.FixedZone("offset", 8*60*60))
		if err := store.PutRepositoryActivity(ctx, 42, older); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetRepository(ctx, 42)
		if err != nil || !reflect.DeepEqual(stored.Activity, &activity) {
			t.Fatal("older/equal fetch overwrote the first newer cache")
		}
	}
	activity.FetchedAt = activity.FetchedAt.Add(time.Nanosecond)
	activity.DefaultBranch = "new-default"
	if err := store.PutRepositoryActivity(ctx, 42, activity); err != nil {
		t.Fatal(err)
	}
	stored, err := store.GetRepository(ctx, 42)
	if err != nil || !reflect.DeepEqual(stored.Activity, &activity) {
		t.Fatal("newer activity did not replace the cache")
	}
}

func TestActivityCandidatesPrioritizeFocusThenUnknownAndRespectCohortAndFreshness(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	cutoff := activityFixture(1).FetchedAt
	start := time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC)
	for id := int64(1); id <= 12; id++ {
		observation := domain.RepositoryObservation{
			GitHubRepoID: id, FullName: fmt.Sprintf("owner/candidate-%d", id), Source: domain.DiscoverySourceManual,
			DiscoveredAt: start.Add(time.Duration(id) * time.Hour), IsFocus: pointer(id == 1 || id == 2),
		}
		if id == 7 {
			observation.MonitoringStatus = domain.MonitoringPaused
		}
		if id == 8 {
			observation.GitHubStatus = domain.GitHubPrivate
		}
		if id == 9 {
			observation.GitHubStatus = domain.GitHubArchived
		}
		if id == 10 {
			observation.GitHubStatus = domain.GitHubUnreachable
		}
		if id == 11 {
			observation.MonitoringStatus = domain.MonitoringStopped
		}
		if id == 12 {
			observation.GitHubStatus = domain.GitHubDeleted
		}
		if _, _, err := store.UpsertRepository(ctx, observation); err != nil {
			t.Fatal(err)
		}
		if id == 1 || id == 3 || id == 4 || id == 5 {
			activity := activityFixture(id)
			if id == 1 || id == 3 {
				activity.FetchedAt = cutoff.Add(-time.Second)
			}
			if id == 5 {
				activity.FetchedAt = cutoff.Add(time.Second)
			}
			if err := store.PutRepositoryActivity(ctx, id, activity); err != nil {
				t.Fatal(err)
			}
		}
	}
	ids := func(items []domain.Repository) []int64 {
		result := []int64{}
		for _, item := range items {
			result = append(result, item.GitHubRepoID)
		}
		return result
	}
	items, err := store.ActivityCandidates(ctx, nil, nil, cutoff, 500)
	if err != nil || !reflect.DeepEqual(ids(items), []int64{2, 1, 9, 6, 3}) {
		t.Fatalf("candidate priority/freshness=%v err=%v", ids(items), err)
	}
	items, err = store.ActivityCandidates(ctx, nil, nil, cutoff, 2)
	if err != nil || !reflect.DeepEqual(ids(items), []int64{2, 1}) {
		t.Fatal("limit not applied after priority")
	}
	since, until := start.Add(3*time.Hour), start.Add(9*time.Hour)
	items, err = store.ActivityCandidates(ctx, &since, &until, cutoff, 500)
	if err != nil || !reflect.DeepEqual(ids(items), []int64{6, 3}) {
		t.Fatalf("cohort bounds=%v err=%v", ids(items), err)
	}
	for _, limit := range []int{-1, 0, 501} {
		if _, err := store.ActivityCandidates(ctx, nil, nil, cutoff, limit); !errors.Is(err, corestore.ErrInvalid) {
			t.Fatalf("bad limit %d accepted", limit)
		}
	}
	for _, pair := range [][2]*time.Time{{&until, &since}, {&since, &since}, {pointer(time.Time{}), nil}} {
		if _, err := store.ActivityCandidates(ctx, pair[0], pair[1], cutoff, 1); !errors.Is(err, corestore.ErrInvalid) {
			t.Fatal("invalid cohort accepted")
		}
	}
	if _, err := store.ActivityCandidates(ctx, nil, nil, time.Time{}, 1); !errors.Is(err, corestore.ErrInvalid) {
		t.Fatal("missing freshness cutoff accepted")
	}
}

func TestActivityMigrationFromV5PreservesAllMetadataHistoryAndOrphans(t *testing.T) {
	db, _ := newV3MigrationFixture(t)
	script, err := fs.ReadFile(migrations.Files, "0004_github_trending_source.sql")
	if err != nil {
		t.Fatal(err)
	}
	if err := applyRepositoryRebuildMigration(context.Background(), db, 4, string(script)); err != nil {
		t.Fatal(err)
	}
	script, err = fs.ReadFile(migrations.Files, "0005_repository_tags.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version=5; UPDATE repositories SET github_topics_json='["agent","skills"]',research_tags_json='["投资","中文写作"]' WHERE github_repo_id=42`); err != nil {
		t.Fatal(err)
	}
	before := migrationEvidence(t, db)
	oldRepositories := migrationRows(t, db, "SELECT "+repositoryColumnsV5+" FROM repositories ORDER BY github_repo_id")
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	assertMigrationSettings(t, db, 7, 0)
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) || !reflect.DeepEqual(oldRepositories, migrationRows(t, db, "SELECT "+repositoryColumnsV5+" FROM repositories ORDER BY github_repo_id")) {
		t.Fatal("activity migration changed existing metadata, tags, schema, history, or orphan mappings")
	}
	var activity sql.NullString
	if err := db.QueryRow("SELECT activity_json FROM repositories WHERE github_repo_id=42").Scan(&activity); err != nil || activity.Valid {
		t.Fatal("migration fabricated zero activity")
	}
	for _, value := range []string{"broken", "null", "[]", "42", `"text"`} {
		if _, err := db.Exec("UPDATE repositories SET activity_json=? WHERE github_repo_id=42", value); err == nil {
			t.Fatalf("invalid activity JSON accepted: %s", value)
		}
	}
	if err := applyMigrations(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, migrationEvidence(t, db)) {
		t.Fatal("repeated migration changed stored evidence")
	}
}

func TestActivityUpdateFailureRollsBackWithoutDestroyingThePreviousCache(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	addRepository(t, store, 42, "owner/activity")
	activity := activityFixture(42)
	if err := store.PutRepositoryActivity(ctx, 42, activity); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`CREATE TRIGGER reject_activity_update BEFORE UPDATE OF activity_json ON repositories
		BEGIN SELECT RAISE(ABORT, 'simulated write failure'); END`); err != nil {
		t.Fatal(err)
	}
	newer := activity
	newer.FetchedAt = newer.FetchedAt.Add(time.Second)
	newer.DefaultBranch = "new-main"
	if err := store.PutRepositoryActivity(ctx, 42, newer); err == nil {
		t.Fatal("failed cache update reported success")
	}
	stored, err := store.GetRepository(ctx, 42)
	if err != nil || !reflect.DeepEqual(stored.Activity, &activity) {
		t.Fatal("failed update destroyed the existing activity cache")
	}
	if _, err := store.db.Exec("DROP TRIGGER reject_activity_update"); err != nil {
		t.Fatal(err)
	}
	if err := store.PutRepositoryActivity(ctx, 42, newer); err != nil {
		t.Fatal("failed transaction prevented a later valid update:", err)
	}
}

func TestActivityNormalizesUTCAndAllowsFiveHundredCompleteOrTruncatedCommits(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	zone := time.FixedZone("Asia/Shanghai", 8*60*60)
	for _, complete := range []bool{true, false} {
		id := int64(1)
		if complete {
			id = 2
		}
		addRepository(t, store, id, fmt.Sprintf("owner/utc-%d", id))
		activity := activityFixture(id)
		activity.WindowEnd = time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC).In(zone)
		activity.WindowStart = time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC).In(zone)
		activity.FetchedAt = activity.WindowEnd.Add(time.Minute)
		activity.LatestCommitAt = pointer(activity.WindowEnd.Add(-time.Hour))
		activity.Commits, activity.ActiveDays, activity.Complete = 500, 1, complete
		for index := range activity.Daily {
			activity.Daily[index] = domain.ActivityDay{Date: activity.WindowStart.UTC().AddDate(0, 0, index).Format(time.DateOnly)}
		}
		activity.Daily[28].Count = 500 // Leap day; the current UTC date has only just started.
		if err := store.PutRepositoryActivity(ctx, id, activity); err != nil {
			t.Fatal(err)
		}
		stored, err := store.GetRepository(ctx, id)
		if err != nil || stored.Activity.WindowStart.Location() != time.UTC || stored.Activity.WindowEnd.Location() != time.UTC ||
			stored.Activity.LatestCommitAt.Location() != time.UTC || stored.Activity.Daily[28].Date != "2024-02-29" || stored.Activity.Commits != 500 || stored.Activity.Complete != complete {
			t.Fatalf("UTC/leap-day normalization = %+v err=%v", stored.Activity, err)
		}
	}
}

func TestActivityCandidatesHaveDeterministicIDTiesAndOneSidedDateBounds(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for _, id := range []int64{1, 3, 2} {
		addRepository(t, store, id, fmt.Sprintf("owner/tie-%d", id))
	}
	firstSeen := testNow.Add(-time.Hour)
	items, err := store.ActivityCandidates(ctx, &firstSeen, nil, testNow, 2)
	if err != nil || len(items) != 2 || items[0].GitHubRepoID != 3 || items[1].GitHubRepoID != 2 {
		t.Fatalf("stable ID tie order = %+v, err=%v", items, err)
	}
	items, err = store.ActivityCandidates(ctx, nil, &firstSeen, testNow, 500)
	if err != nil || len(items) != 0 {
		t.Fatal("exclusive until date did not exclude the exact first_seen instant")
	}
}

func TestActivityDoesNotAssumeCommitGraphOrderIsTimestampOrder(t *testing.T) {
	store, _ := newTestStore(t)
	ctx := context.Background()
	for index, latestOffset := range []time.Duration{-time.Hour, 0, time.Hour} {
		id := int64(index + 1)
		addRepository(t, store, id, fmt.Sprintf("owner/nonmonotonic-%d", id))
		activity := activityFixture(id)
		// The default-branch head can have an older committer date than a
		// reachable parent. LatestCommitAt must not become max(Daily.Date).
		activity.LatestCommitAt = pointer(activity.WindowStart.Add(latestOffset))
		if err := store.PutRepositoryActivity(ctx, id, activity); err != nil {
			t.Fatalf("valid nonmonotonic Git dates rejected: %v", err)
		}
		stored, err := store.GetRepository(ctx, id)
		if err != nil || !stored.Activity.LatestCommitAt.Equal(*activity.LatestCommitAt) {
			t.Fatal("store invented a latest-commit timestamp from aggregate dates")
		}
	}
}

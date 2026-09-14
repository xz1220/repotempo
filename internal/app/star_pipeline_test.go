package app

import (
	"context"
	"encoding/csv"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/web"
)

// Exercise real GitHub HTTP decoding -> snapshot job -> SQLite -> filtered
// library/CSV/detail. Missing observations must never become zeros or gains.
func TestStarCollectionFeedsNumericSortingFiltersAndExports(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 9, 15, 1, 0, 0, 0, time.UTC)
	names := map[int64]string{1: "fixture/blocked", 2: "fixture/large", 3: "fixture/fast", 4: "fixture/zero", 5: "fixture/new"}
	stars := map[int64]int64{2: 2000, 3: 1000, 4: 0, 5: 500}
	runtime, clock := importRuntime(t, func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repositories/"), 10, 64)
		w.Header().Set("X-RateLimit-Remaining", "4900")
		if id == 1 {
			w.WriteHeader(403)
			fmt.Fprint(w, `{"message":"Repository access blocked"}`)
			return
		}
		if _, ok := stars[id]; !ok {
			http.NotFound(w, r)
			return
		}
		tag := "ai"
		if id == 4 {
			tag = "tools"
		}
		fmt.Fprintf(w, `{"id":%d,"full_name":%q,"html_url":"https://github.com/%s","stargazers_count":%d,"topics":[%q]}`, id, names[id], names[id], stars[id], tag)
	})
	clock.Store(now.Unix())
	runtime.settings.TopicsConfig = t.TempDir() + "/topics.yaml"
	writeFixture(t, runtime.settings.TopicsConfig, "version: 1\ntopics:\n  - slug: coding-agents\n    name: Coding agents\n    status: active\n")
	for id, name := range names {
		seen := now.AddDate(0, 0, -40)
		if id == 5 {
			seen = now
		}
		if _, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{GitHubRepoID: id, FullName: name, Source: domain.DiscoverySourceManual, DiscoveredAt: seen}); err != nil {
			t.Fatal(err)
		}
		if id == 5 {
			continue
		}
		for _, days := range []int{1, 7, 30} {
			value := int64(0)
			switch id {
			case 1:
				value = 10000
			case 2:
				value = 2000 - int64(days)
			case 3:
				value = 1000 - int64(days)*10
			}
			at := now.AddDate(0, 0, -days)
			if _, err := runtime.store.PutDailySnapshot(ctx, domain.DailySnapshot{RepositoryID: id, SnapshotDate: domain.ShanghaiDate(at), CapturedAt: at, StarCount: &value, FetchStatus: domain.FetchSuccess}); err != nil {
				t.Fatal(err)
			}
		}
	}
	report, err := runtime.Snapshot(ctx, false)
	if err != nil || report.TargetCount != 5 || report.SuccessCount != 4 || report.FailureCount != 1 {
		t.Fatalf("snapshot job: %+v %v", report, err)
	}
	adapter := WebAdapter{Store: runtime.store}
	query := web.RepositoryQuery{AsOf: now, WindowDays: 7, Sort: "stars", Limit: 20}
	checkOrder := func(q web.RepositoryQuery, want []int64) web.RepositoryPage {
		t.Helper()
		page, err := adapter.ListRepositoryTrends(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		got := []int64{}
		for _, item := range page.Items {
			got = append(got, item.ID)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%+v order=%v want=%v", q, got, want)
		}
		return page
	}
	page := checkOrder(query, []int64{1, 2, 3, 5, 4})
	if !page.Items[0].IsStale || page.Items[0].CurrentStars != nil || page.Items[0].StarDelta != nil || *page.Items[0].LastObservedStars != 10000 {
		t.Fatal("blocked repository's stale Stars became a current observation")
	}
	if page.Items[3].StarDelta != nil || page.Items[4].CurrentStars == nil || *page.Items[4].CurrentStars != 0 {
		t.Fatal("missing growth or real zero was misrepresented")
	}
	query.Tag = "ai"
	checkOrder(query, []int64{2, 3, 5})
	query.Sort = "delta"
	checkOrder(query, []int64{3, 2, 5})
	query.Search = "fast"
	for _, days := range []int{1, 7, 30} {
		query.WindowDays = days
		item := checkOrder(query, []int64{3}).Items[0]
		if item.CurrentStars == nil || *item.CurrentStars != 1000 || item.StarDelta == nil || *item.StarDelta != int64(days)*10 {
			t.Fatalf("%dd values: %+v", days, item)
		}
	}
	query.Search = ""
	query.OnlyNew = true
	checkOrder(query, []int64{5})
	query = web.RepositoryQuery{AsOf: now, WindowDays: 7, Sort: "stars", Limit: 2}
	checkOrder(query, []int64{1, 2})
	query.Offset = 2
	checkOrder(query, []int64{3, 5})
	query.Offset = 4
	checkOrder(query, []int64{4})
	handler, err := web.New(adapter, web.Options{Now: func() time.Time { return now }, Locale: "en"})
	if err != nil {
		t.Fatal(err)
	}
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s=%d", path, w.Code)
		}
		return w
	}
	for _, days := range []int{1, 7, 30} {
		suffix := fmt.Sprintf("?date=2026-09-15&view=all&new=0&q=fast&tag=ai&period=%dd&sort=stars", days)
		list := html.UnescapeString(get("/repositories" + suffix).Body.String())
		if !strings.Contains(list, `<strong>1,000</strong>`) || !strings.Contains(list, fmt.Sprintf(">+%d</strong>", days*10)) {
			t.Fatalf("list not showing collected Stars for %dd", days)
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(get("/repositories/export"+suffix).Body.String(), "\ufeff"))).ReadAll()
		if err != nil || len(rows) != 2 || rows[1][4] != "1000" || rows[1][5] != strconv.Itoa(days*10) {
			t.Fatalf("CSV differs from selected scope: %v %v", rows, err)
		}
	}
	detail, err := adapter.GetRepositoryDetail(ctx, 3, now)
	if err != nil || detail.Repository.CurrentStars == nil || *detail.Repository.CurrentStars != 1000 || *detail.Repository.Delta7D != 70 {
		t.Fatalf("detail disagrees: %+v %v", detail.Repository, err)
	}
	// Date changes cannot reuse today's newly collected value for a missing day.
	query = web.RepositoryQuery{AsOf: now.AddDate(0, 0, -2), WindowDays: 1, Sort: "stars", Search: "fast", Limit: 20}
	historic := checkOrder(query, []int64{3}).Items[0]
	if historic.CurrentStars != nil || historic.StarDelta != nil {
		t.Fatal("missing historical date was filled with another day's value")
	}
	repeated, err := runtime.Snapshot(ctx, false)
	if err != nil || repeated.SkippedCount != 4 || repeated.FailureCount != 1 || repeated.SuccessCount != 0 {
		t.Fatalf("resume repeated successful snapshots: %+v %v", repeated, err)
	}
}

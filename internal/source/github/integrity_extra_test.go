package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/config"
)

func TestSearchPartitionAtMaxDepthReturnsPartialHitsAndIntegrityError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page %q: %v", r.URL.Query().Get("page"), err)
			writeSearch(t, w, 0, false, nil)
			return
		}

		switch query {
		case "stars:0..2000":
			if page != 1 {
				t.Errorf("root query page = %d, want 1", page)
			}
			writeSearch(t, w, 2001, false, []githubRepository{repoItem(9000, 2000)})
		case "stars:0..1000":
			writeSearch(t, w, 1001, false, []githubRepository{repoItem(int64(100+page), int64(1000-page))})
		case "stars:1001..2000":
			if page != 1 {
				t.Errorf("right partition page = %d, want 1", page)
			}
			writeSearch(t, w, 1, false, []githubRepository{repoItem(2001, 1500)})
		default:
			t.Errorf("unexpected query %q", query)
			writeSearch(t, w, 0, false, nil)
		}
	}))
	defer server.Close()

	client := githubTestClient(t, server, nil)
	profile := profileWith(config.SearchQuery{
		Stars: config.NumberRange{Min: intPtr(0), Max: intPtr(2000)},
	})
	profile.Partition = config.Partition{By: "stars", MaxDepth: 1}

	result, err := client.SearchProfile(context.Background(), profile, time.Now())
	var integrity *SearchIntegrityError
	if !errors.As(err, &integrity) {
		t.Fatalf("error = %#v, want SearchIntegrityError", err)
	}
	if !integrity.Truncated || integrity.Incomplete {
		t.Fatalf("integrity error = %+v, want truncated-only failure", integrity)
	}
	if len(integrity.Queries) != 1 || integrity.Queries[0] != "stars:0..1000" {
		t.Fatalf("integrity queries = %v, want [stars:0..1000]", integrity.Queries)
	}
	if !result.Truncated || result.IncompleteResults {
		t.Fatalf("result integrity flags = truncated:%v incomplete:%v", result.Truncated, result.IncompleteResults)
	}
	if len(result.Reports) != 3 || !result.Reports[0].Split || !result.Reports[1].Truncated || result.Reports[1].Depth != 1 || result.Reports[1].Pages != 10 {
		t.Fatalf("reports = %+v", result.Reports)
	}
	if got, want := len(result.Hits), 11; got != want {
		t.Fatalf("partial hits = %d, want %d", got, want)
	}
	seen := make(map[int64]bool, len(result.Hits))
	for _, hit := range result.Hits {
		seen[hit.Repository.ID] = true
	}
	for id := int64(101); id <= 110; id++ {
		if !seen[id] {
			t.Errorf("partial hits do not contain repository %d", id)
		}
	}
	if !seen[2001] {
		t.Error("partial hits do not contain the complete sibling partition result")
	}
	if seen[9000] {
		t.Error("partial hits unexpectedly contain the superseded root partition probe")
	}
}

func TestSearchWaitsForCachedResourceResetWhenRemainingIsZero(t *testing.T) {
	start := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	reset := start.Add(5 * time.Second)
	clock := &fakeClock{now: start}
	var requests atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestNumber := requests.Add(1)
		w.Header().Set("X-RateLimit-Resource", "search")
		if requestNumber == 1 {
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		} else {
			w.Header().Set("X-RateLimit-Remaining", "29")
		}
		writeSearch(t, w, 0, false, nil)
	}))
	defer server.Close()

	client := githubTestClient(t, server, func(options *ClientOptions) {
		options.Now = clock.Now
		options.Sleep = clock.Sleep
	})
	if _, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "first"}), clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got := client.RateLimits()[ResourceSearch].Remaining; got != 0 {
		t.Fatalf("cached search remaining = %d, want 0", got)
	}
	if _, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "second"}), clock.Now()); err != nil {
		t.Fatal(err)
	}

	if got := requests.Load(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	wantWait := reset.Sub(start) + time.Second
	if len(clock.sleeps) != 1 || clock.sleeps[0] != wantWait {
		t.Fatalf("waits = %v, want [%v]", clock.sleeps, wantWait)
	}
	if got, want := clock.Now(), reset.Add(time.Second); !got.Equal(want) {
		t.Fatalf("clock after wait = %v, want %v", got, want)
	}
}

func TestSearchEmptyIntermediatePageIsExplicitlyIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 1 {
			writeSearch(t, w, 200, false, []githubRepository{repoItem(1, 100)})
			return
		}
		writeSearch(t, w, 200, false, nil)
	}))
	defer server.Close()

	client := githubTestClient(t, server, nil)
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "radar"}), time.Now())
	var integrity *SearchIntegrityError
	if !errors.As(err, &integrity) || !integrity.Incomplete {
		t.Fatalf("error = %#v, want incomplete SearchIntegrityError", err)
	}
	if !result.IncompleteResults || len(result.Hits) != 1 || !result.Reports[0].IncompleteResults {
		t.Fatalf("result = %#v", result)
	}
}

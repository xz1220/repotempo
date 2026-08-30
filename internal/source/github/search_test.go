package github

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/xz1220/github-radar/internal/config"
)

func intPtr(value int64) *int64 { return &value }
func boolPtr(value bool) *bool  { return &value }

func profileWith(query config.SearchQuery) config.SearchProfile {
	return config.SearchProfile{
		Name:      "test",
		Schedule:  "daily",
		Sort:      "stars",
		Order:     "desc",
		Queries:   []config.SearchQuery{query},
		Partition: config.Partition{By: "none"},
	}
}

func repoItem(id, stars int64) githubRepository {
	description := "repository"
	language := "Go"
	return githubRepository{
		ID:              id,
		NodeID:          "R_" + strconv.FormatInt(id, 10),
		FullName:        "owner/repo-" + strconv.FormatInt(id, 10),
		HTMLURL:         "https://github.com/owner/repo-" + strconv.FormatInt(id, 10),
		Description:     &description,
		Language:        &language,
		StargazersCount: stars,
		ForksCount:      1,
		CreatedAt:       "2026-01-01T00:00:00Z",
		UpdatedAt:       "2026-08-01T00:00:00Z",
		PushedAt:        "2026-08-01T00:00:00Z",
	}
}

func writeSearch(t *testing.T, w http.ResponseWriter, total int, incomplete bool, items []githubRepository) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Resource", "search")
	if w.Header().Get("X-RateLimit-Remaining") == "" {
		w.Header().Set("X-RateLimit-Remaining", "29")
	}
	if err := json.NewEncoder(w).Encode(map[string]any{
		"total_count":        total,
		"incomplete_results": incomplete,
		"items":              items,
	}); err != nil {
		t.Error(err)
	}
}

func TestSearchQueryEncodingAndFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "github-search", "page.json"))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q, want 100", got)
		}
		if got := r.URL.Query().Get("page"); got != "1" {
			t.Errorf("page = %q, want 1", got)
		}
		wantQuery := `stars:>=100 topic:"ai agent" language:C++ fork:false archived:false`
		if got := r.URL.Query().Get("q"); got != wantQuery {
			t.Errorf("q = %q, want %q", got, wantQuery)
		}
		if !strings.Contains(r.URL.RawQuery, "C%2B%2B") {
			t.Errorf("raw query did not URL-encode C++: %s", r.URL.RawQuery)
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{
		Stars:    config.NumberRange{Min: intPtr(100)},
		Topic:    "ai agent",
		Language: "C++",
		Fork:     boolPtr(false),
		Archived: boolPtr(false),
	}), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(result.Hits), 2; got != want {
		t.Fatalf("hits = %d, want %d", got, want)
	}
}

func TestSearchPaginationAndSerialInterval(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		requests.Add(1)
		if page == 1 {
			items := make([]githubRepository, 100)
			for index := range items {
				items[index] = repoItem(int64(index+1), int64(1000-index))
			}
			writeSearch(t, w, 101, false, items)
			return
		}
		writeSearch(t, w, 101, false, []githubRepository{repoItem(101, 900)})
	}))
	defer server.Close()
	client := githubTestClient(t, server, func(options *ClientOptions) {
		options.Now = clock.Now
		options.Sleep = clock.Sleep
		options.SearchInterval = 2 * time.Second
	})
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Stars: config.NumberRange{Min: intPtr(1)}}), clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 101 || result.Reports[0].Pages != 2 || requests.Load() != 2 {
		t.Fatalf("result = hits %d reports %+v requests %d", len(result.Hits), result.Reports, requests.Load())
	}
	if len(clock.sleeps) != 1 || clock.sleeps[0] != 2*time.Second {
		t.Fatalf("search limiter sleeps = %v", clock.sleeps)
	}
}

func TestSearchRecognizesThousandItemBoundary(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.ParseInt(r.URL.Query().Get("page"), 10, 64)
		writeSearch(t, w, 1001, false, []githubRepository{repoItem(page, 1000-page)})
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Stars: config.NumberRange{Min: intPtr(1)}}), time.Now())
	var integrity *SearchIntegrityError
	if !errors.As(err, &integrity) || !integrity.Truncated {
		t.Fatalf("error = %#v, want truncated integrity error", err)
	}
	if !result.Truncated || result.Reports[0].Pages != 10 {
		t.Fatalf("result = %+v", result)
	}
}

func TestSearchStarPartition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("q")
		switch query {
		case "stars:>=100 fork:false archived:false":
			writeSearch(t, w, 1501, false, []githubRepository{repoItem(9999, 2000)})
		case "stars:100..1050 fork:false archived:false":
			writeSearch(t, w, 800, false, []githubRepository{repoItem(1, 500)})
		case "stars:1051..2000 fork:false archived:false":
			writeSearch(t, w, 701, false, []githubRepository{repoItem(2, 1500)})
		default:
			t.Errorf("unexpected query %q", query)
			writeSearch(t, w, 0, false, nil)
		}
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	profile := profileWith(config.SearchQuery{
		Stars:    config.NumberRange{Min: intPtr(100)},
		Fork:     boolPtr(false),
		Archived: boolPtr(false),
	})
	profile.Partition = config.Partition{By: "stars", MaxDepth: 4}
	result, err := client.SearchProfile(context.Background(), profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if result.Truncated || len(result.Hits) != 2 || !result.Reports[0].Split {
		t.Fatalf("result = %+v", result)
	}
}

func TestSearchStarPartitionProbesMaximumWhenSortedByUpdated(t *testing.T) {
	var sawProbe bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %q, want 100 for every collection/probe request", got)
		}
		query := r.URL.Query().Get("q")
		if query == "stars:>=100" && r.URL.Query().Get("sort") == "stars" {
			sawProbe = true
			writeSearch(t, w, 1200, false, []githubRepository{repoItem(2000, 2000)})
			return
		}
		switch query {
		case "stars:>=100":
			writeSearch(t, w, 1200, false, []githubRepository{repoItem(999, 500)})
		case "stars:100..1050":
			writeSearch(t, w, 600, false, []githubRepository{repoItem(1, 500)})
		case "stars:1051..2000":
			writeSearch(t, w, 600, false, []githubRepository{repoItem(2, 1500)})
		default:
			t.Errorf("unexpected query %q", query)
			writeSearch(t, w, 0, false, nil)
		}
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	profile := profileWith(config.SearchQuery{Stars: config.NumberRange{Min: intPtr(100)}})
	profile.Sort = "updated"
	profile.Partition = config.Partition{By: "stars", MaxDepth: 4}
	result, err := client.SearchProfile(context.Background(), profile, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !sawProbe || len(result.Hits) != 2 {
		t.Fatalf("saw probe = %v, result = %+v", sawProbe, result)
	}
}

func TestDatePartitionStrategy(t *testing.T) {
	from := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	children, err := splitDate(Query{Created: TimeRange{From: &from, To: &to}}, "created", to)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := children[0].Created.To.Format("2006-01-02"), "2026-08-15"; got != want {
		t.Fatalf("left end = %s, want %s", got, want)
	}
	if got, want := children[1].Created.From.Format("2006-01-02"), "2026-08-16"; got != want {
		t.Fatalf("right start = %s, want %s", got, want)
	}
}

func TestSearchIncompleteResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSearch(t, w, 1, true, []githubRepository{repoItem(1, 10)})
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "agent"}), time.Now())
	var integrity *SearchIntegrityError
	if !errors.As(err, &integrity) || !integrity.Incomplete || !result.IncompleteResults || len(result.Hits) != 1 {
		t.Fatalf("result = %+v, error = %#v", result, err)
	}
}

func TestSearchFiltersForksAndArchivedDefensively(t *testing.T) {
	fork := repoItem(1, 10)
	fork.Fork = true
	archived := repoItem(2, 10)
	archived.Archived = true
	active := repoItem(3, 10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeSearch(t, w, 3, false, []githubRepository{fork, archived, active})
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "agent", Fork: boolPtr(false), Archived: boolPtr(false)}), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Hits) != 1 || result.Hits[0].Repository.ID != 3 {
		t.Fatalf("hits = %+v", result.Hits)
	}
}

func TestSearchStatusErrorsAndTimeout(t *testing.T) {
	for _, status := range []int{http.StatusNotModified, http.StatusForbidden, http.StatusUnprocessableEntity, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"message":"failed"}`))
			}))
			defer server.Close()
			client := githubTestClient(t, server, nil)
			_, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "agent"}), time.Now())
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != status {
				t.Fatalf("error = %#v", err)
			}
		})
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		writeSearch(t, w, 0, false, nil)
	}))
	defer server.Close()
	client := githubTestClient(t, server, func(options *ClientOptions) { options.Timeout = 5 * time.Millisecond })
	_, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "agent"}), time.Now())
	var transport *TransportError
	if !errors.As(err, &transport) || transport.Code != CodeTimeout {
		t.Fatalf("timeout error = %#v", err)
	}
}

func TestMergeSearchResultsAcrossProfiles(t *testing.T) {
	repository := repoItem(1, 10)
	sourceRepository, err := repository.toSource()
	if err != nil {
		t.Fatal(err)
	}
	merged := MergeSearchResults(
		SearchResult{Hits: []SearchHit{{Repository: sourceRepository, Profile: "one", MatchedQueries: []string{"q1"}, MatchedProfiles: []string{"one"}}}},
		SearchResult{Hits: []SearchHit{{Repository: sourceRepository, Profile: "two", MatchedQueries: []string{"q2"}, MatchedProfiles: []string{"two"}}}},
	)
	if len(merged) != 1 || strings.Join(merged[0].MatchedQueries, ",") != "q1,q2" || strings.Join(merged[0].MatchedProfiles, ",") != "one,two" || merged[0].Profile != "one" {
		t.Fatalf("merged = %+v", merged)
	}
}

func TestQueryStringRoundTripsThroughURLValues(t *testing.T) {
	query := Query{Text: "agent memory", Topic: "C++ tools", Fork: boolPtr(false)}
	values := make(url.Values)
	values.Set("q", query.String())
	parsed, err := url.ParseQuery(values.Encode())
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Get("q") != query.String() {
		t.Fatalf("round trip = %q, want %q", parsed.Get("q"), query.String())
	}
}

func TestClientTracksCoreAndSearchRateLimitsSeparately(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			w.Header().Set("X-RateLimit-Resource", "search")
			w.Header().Set("X-RateLimit-Remaining", "27")
			writeSearch(t, w, 0, false, nil)
			return
		}
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Remaining", "4998")
		_, _ = w.Write(repositoryFixture(t))
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	if _, err := client.FetchRepositoryByID(context.Background(), 1296269, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := client.SearchProfile(context.Background(), profileWith(config.SearchQuery{Text: "agent"}), time.Now()); err != nil {
		t.Fatal(err)
	}
	rates := client.RateLimits()
	if rates[ResourceCore].Remaining != 4998 || rates[ResourceSearch].Remaining != 27 {
		t.Fatalf("rates = %+v", rates)
	}
}

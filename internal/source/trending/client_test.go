package trending

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func row(name, total, gain, period string) string {
	parts := strings.Split(name, "/")
	label := map[string]string{"daily": "today", "weekly": "this week", "monthly": "this month"}[period]
	return fmt.Sprintf(`<article class="extra Box-row"><div><a href="/login?return_to=%%2F%s">Star</a></div>
<h2 class="h3"><a href="/%s"><svg><title>Repository icon</title></svg><span> %s / </span>
 %s </a></h2><p>A project description with numbers 123</p>
<div><a href="/%s/stargazers"><svg aria-label="star"></svg> %s </a>
<a href="/%s/forks">999</a><span>Built by <a href="/builder">Builder</a></span>
<span class="d-inline-block float-sm-right"><svg aria-hidden="true"></svg> %s stars %s </span></div></article>`, name, name, parts[0], parts[1], name, total, name, gain, label)
}

func page(rows string) string {
	return "<!doctype html><html><head><title>Trending repositories on GitHub</title></head><body><main><h1>Trending</h1>" + rows + "</main></body></html>"
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client(), Interval: time.Nanosecond, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFetchWindowsPreservesBoardOrderAndExactNumbers(t *testing.T) {
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trending" || len(r.URL.Query()) != 1 || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("User-Agent") == "" {
			t.Errorf("unexpected request: %s headers=%v", r.URL, r.Header)
		}
		period := r.URL.Query().Get("since")
		requested = append(requested, period)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, page(row("Owner/First.Repo", "9,007,199,254,740,993", "1,234", period)+row("another/second", "5", "0", period)))
	}))
	defer server.Close()
	client := testClient(t, server)
	now := time.Date(2026, 9, 8, 1, 2, 3, 0, time.FixedZone("CST", 8*3600))
	client.now = func() time.Time { return now }
	result, err := client.FetchWindows(context.Background(), []string{"daily", "weekly", "monthly"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(requested, ",") != "daily,weekly,monthly" || len(result.Windows) != 3 {
		t.Fatalf("wrong windows: %+v", result)
	}
	for _, window := range result.Windows {
		if !window.CapturedAt.Equal(now) || window.CapturedAt.Location() != time.UTC || !strings.HasSuffix(window.URL, "since="+window.Period) || window.Error != "" || len(window.Entries) != 2 {
			t.Fatalf("window: %+v", window)
		}
		first, second := window.Entries[0], window.Entries[1]
		if first.FullName != "Owner/First.Repo" || first.Rank != 1 || *first.TotalStars != 9007199254740993 || *first.StarsInPeriod != 1234 || second.Rank != 2 || *second.StarsInPeriod != 0 {
			t.Fatalf("wrong entries: %+v", window.Entries)
		}
	}
}

func TestSingleWindowFailureDoesNotHideOtherListsOrRetry(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Query().Get("since") == "weekly" {
					http.Error(w, "private internal response", status)
					return
				}
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, page(row("owner/repo", "30", "4", r.URL.Query().Get("since"))))
			}))
			defer server.Close()
			result, err := testClient(t, server).FetchWindows(context.Background(), []string{"daily", "weekly", "monthly"})
			if err == nil || calls.Load() != 3 || len(result.Windows) != 3 || result.Windows[0].Error != "" || result.Windows[1].Error == "" || result.Windows[2].Error != "" {
				t.Fatalf("failure not isolated: %+v, %v calls=%d", result, err, calls.Load())
			}
			if strings.Contains(err.Error(), "private internal") {
				t.Fatal("response body leaked into error")
			}
		})
	}
}

func TestRetryAfterDefersRemainingRequestsWithoutHammeringOrigin(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	result, err := testClient(t, server).FetchWindows(context.Background(), []string{"daily", "weekly", "monthly"})
	if err == nil || calls.Load() != 1 || len(result.Windows) != 3 {
		t.Fatalf("rate limit not respected: %+v, %v", result, err)
	}
	for _, window := range result.Windows[1:] {
		if !strings.Contains(window.Error, "Retry-After") {
			t.Fatalf("deferred window not explicit: %+v", window)
		}
	}
}

func TestParseFailuresAreExplicitAndPreserveOriginalRanks(t *testing.T) {
	base, _ := url.Parse("https://github.com/trending?since=daily")
	bad := `<article class="Box-row"><h2><a href="/topics/ai/extra">Wrong</a></h2></article>`
	entries, err := parsePage([]byte(page(row("owner/one", "10", "1", "daily")+bad+row("owner/three", "20", "2", "daily"))), base, "daily")
	if err == nil || !strings.Contains(err.Error(), "row 2") || len(entries) != 2 || entries[1].Rank != 3 {
		t.Fatalf("partial parsed as complete: %+v %v", entries, err)
	}
	entries, err = parsePage([]byte(page(row("owner/one", "10", "1", "daily")+row("OWNER/ONE", "10", "1", "daily"))), base, "daily")
	if err == nil || len(entries) != 1 || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate not diagnosed: %+v %v", entries, err)
	}
}

func TestMalformedAndChallengePagesCannotLookLikeSuccessfulEmptyLists(t *testing.T) {
	base, _ := url.Parse("https://github.com/trending?since=daily")
	for _, body := range []string{
		page(""), `<html><title>Sign in to GitHub</title><form action="/session"></form></html>`,
		`<html><h1>Verify you are human</h1></html>`, `<html><title>Just a moment...</title></html>`,
		page(row("owner/repo", "1.2k", "1", "daily")), page(row("owner/repo", "12,34", "1", "daily")),
		page(row("owner/repo", "9223372036854775808", "1", "daily")),
		page(row("owner/repo", "100", "2", "weekly")),
		page(strings.Replace(row("owner/repo", "10", "1", "daily"), `href="/owner/repo"`, `href="https://evil.example/owner/repo"`, 1)),
		page(strings.Replace(row("owner/repo", "10", "1", "daily"), `href="/owner/repo"`, `href="/owner/other"`, 1)),
		page(`<article class="Box-row"><h2><a href="/owner/repo">owner/repo</a></h2>`),
	} {
		if _, err := parsePage([]byte(body), base, "daily"); err == nil {
			t.Fatalf("accepted malformed/challenge page: %s", body)
		}
	}
}

func TestPartialWindowHasErrorAndRetainsParsedEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(row("owner/repo", "1.2k", "3", "daily")))
	}))
	defer server.Close()
	result, err := testClient(t, server).FetchWindows(context.Background(), []string{"daily"})
	if err == nil || result.Windows[0].Error == "" || len(result.Windows[0].Entries) != 1 || result.Windows[0].Entries[0].TotalStars != nil || *result.Windows[0].Entries[0].StarsInPeriod != 3 {
		t.Fatalf("partial data lost or unmarked: %+v %v", result, err)
	}
}

func TestRejectsOversizedAndNonHTMLResponses(t *testing.T) {
	for _, test := range []struct{ contentType, body string }{
		{"application/json", `{"error":"blocked"}`},
		{"text/html", strings.Repeat("a", maxResponseBytes+1)},
		{"text/html", string([]byte{0xff, 0xfe})},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", test.contentType)
			fmt.Fprint(w, test.body)
		}))
		result, err := testClient(t, server).FetchWindows(context.Background(), []string{"daily"})
		server.Close()
		if err == nil || result.Windows[0].Error == "" {
			t.Fatalf("invalid response succeeded: %+v %v", result, err)
		}
	}
}

func TestCrossOriginRedirectIsNotFollowedAndNoCookiesAreInherited(t *testing.T) {
	var targetCalls atomic.Int64
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent to public HTML endpoint")
		}
		http.Redirect(w, r, target.URL+"/trending?since=daily", http.StatusFound)
	}))
	defer server.Close()
	jar, _ := cookiejar.New(nil)
	origin, _ := url.Parse(server.URL)
	jar.SetCookies(origin, []*http.Cookie{{Name: "session", Value: "sensitive"}})
	httpClient := server.Client()
	httpClient.Jar = jar
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return nil }
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: httpClient, Interval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FetchWindows(context.Background(), []string{"daily"})
	if err == nil || targetCalls.Load() != 0 || !strings.Contains(result.Windows[0].Error, "cross-origin") {
		t.Fatalf("cross-origin followed: %+v %v", result, err)
	}
	if httpClient.Jar == nil {
		t.Fatal("caller-owned client mutated")
	}
}

func TestRequestsArePacedAndCancellationStopsQueuedWork(t *testing.T) {
	var mu sync.Mutex
	var timestamps []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		timestamps = append(timestamps, time.Now())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page(row("owner/repo", "10", "1", r.URL.Query().Get("since"))))
	}))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, Interval: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.FetchWindows(context.Background(), []string{"daily", "weekly", "monthly"}); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	for i := 1; i < len(timestamps); i++ {
		if timestamps[i].Sub(timestamps[i-1]) < 20*time.Millisecond {
			t.Fatalf("requests not paced: %v", timestamps)
		}
	}
	mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.FetchWindows(ctx, []string{"daily"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
}

func TestRequestTimeoutIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, Timeout: 20 * time.Millisecond, Interval: time.Nanosecond})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FetchWindows(context.Background(), []string{"daily"})
	if !errors.Is(err, context.DeadlineExceeded) || len(result.Windows) != 1 || result.Windows[0].Error == "" {
		t.Fatalf("timeout not diagnosed: %+v %v", result, err)
	}
}

func TestRejectsInvalidOriginsAndPeriodsBeforeNetwork(t *testing.T) {
	for _, origin := range []string{"http://github.com/", "https://evil.example/", "https://github.com/other", "https://token@github.com/", "https://github.com/?since=weekly", "https://github.com/#fragment"} {
		if _, err := NewClient(ClientOptions{BaseURL: origin}); err == nil {
			t.Fatalf("accepted origin %s", origin)
		}
	}
	client, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, periods := range [][]string{nil, {"daily", "daily"}, {"daily", "yearly"}, {"daily&language=go"}} {
		if _, err := client.FetchWindows(context.Background(), periods); err == nil {
			t.Fatalf("accepted invalid periods: %v", periods)
		}
	}
}

// Deliberately opt-in: normal tests never request GitHub. Run this only for
// release validation or to diagnose a changed public Trending layout.
func TestLiveTrendingWindows(t *testing.T) {
	if os.Getenv("GITHUB_RADAR_TRENDING_LIVE") != "1" {
		t.Skip("set GITHUB_RADAR_TRENDING_LIVE=1 for the public-page smoke check")
	}
	client, err := NewClient(ClientOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()
	result, err := client.FetchWindows(ctx, []string{"daily", "weekly", "monthly"})
	for _, window := range result.Windows {
		t.Logf("%s: %d entries, captured=%s, error=%q", window.Period, len(window.Entries), window.CapturedAt.Format(time.RFC3339), window.Error)
	}
	if err != nil {
		t.Fatal(err)
	}
}

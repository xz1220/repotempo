package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var activityNow = time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)

func activityTestClient(t *testing.T, handler http.HandlerFunc, mutate func(*ClientOptions)) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return githubTestClient(t, server, func(options *ClientOptions) {
		options.Now = func() time.Time { return activityNow }
		if mutate != nil {
			mutate(options)
		}
	})
}

func activityMetadata(w http.ResponseWriter) {
	_, _ = fmt.Fprint(w, `{"id":42,"full_name":"new-owner/renamed-repo","default_branch":"release/main","private":false,"fork":true}`)
}

func activityCommitJSON(id int, date string) string {
	return fmt.Sprintf(`{"sha":"%040x","commit":{"author":{"date":"1999-01-01T00:00:00Z"},"committer":{"date":%q}}}`, id, date)
}

func activityHead(w http.ResponseWriter) {
	_, _ = fmt.Fprintf(w, "[%s]", activityCommitJSON(1, "2026-09-08T11:00:00Z"))
}

func activityNext(w http.ResponseWriter, r *http.Request, page int) {
	next := *r.URL
	values := next.Query()
	values.Set("page", strconv.Itoa(page))
	next.RawQuery = values.Encode()
	w.Header().Set("Link", "<"+next.String()+">; rel=\"next\"")
}

func TestFetchActivityPinsRenamedForkDeduplicatesAndBinsUTC(t *testing.T) {
	requests := 0
	client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" || r.Header.Get("If-None-Match") != "" {
			t.Errorf("unexpected authentication or ETag headers")
		}
		if r.URL.Path == "/repositories/42" {
			activityMetadata(w)
			return
		}
		if r.URL.Path != "/repos/new-owner/renamed-repo/commits" {
			t.Errorf("did not use current name: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("until") != activityNow.Format(time.RFC3339) {
			t.Error("omitted cutoff")
		}
		if q.Get("per_page") == "1" {
			if q.Get("sha") != "release/main" || q.Has("since") {
				t.Error("branch resolution must include idle history")
			}
			activityHead(w)
			return
		}
		if q.Get("sha") != fmt.Sprintf("%040x", 1) || q.Get("since") != "2026-08-10T00:00:00Z" || q.Get("per_page") != "100" {
			t.Errorf("wrong pinned scope: %v", q)
		}
		switch q.Get("page") {
		case "1":
			activityNext(w, r, 2)
			_, _ = fmt.Fprintf(w, "[%s,%s]", activityCommitJSON(1, "2026-09-08T11:00:00Z"), activityCommitJSON(2, "2026-08-10T00:00:00Z"))
		case "2":
			_, _ = fmt.Fprintf(w, "[%s,%s]", activityCommitJSON(2, "2026-08-10T00:00:00Z"), activityCommitJSON(3, "2026-09-08T07:30:00+08:00"))
		default:
			t.Errorf("unexpected page %q", q.Get("page"))
		}
	}, nil)
	got, err := client.FetchActivity(context.Background(), 42, activityNow.In(time.FixedZone("CST", 8*3600)), 3)
	if err != nil {
		t.Fatal(err)
	}
	if requests != 4 || !got.Complete || !got.IsFork || got.RepositoryID != 42 || got.DefaultBranch != "release/main" || got.HeadSHA != fmt.Sprintf("%040x", 1) || got.Commits != 3 || got.ActiveDays != 3 {
		t.Fatalf("activity=%+v, requests=%d", got, requests)
	}
	if len(got.Daily) != 30 || got.Daily[0].Date != "2026-08-10" || got.Daily[0].Count != 1 || got.Daily[28].Date != "2026-09-07" || got.Daily[28].Count != 1 || got.Daily[29].Count != 1 || got.Daily[1].Count != 0 {
		t.Fatalf("wrong UTC bins: %+v", got.Daily)
	}
	if got.LatestCommitAt == nil || got.LatestCommitAt.Format(time.RFC3339) != "2026-09-08T11:00:00Z" || !got.FetchedAt.Equal(activityNow) || !got.WindowEnd.Equal(activityNow) {
		t.Fatalf("wrong timestamps: %+v", got)
	}
}

func TestFetchActivityPageBudgetAndExactFullFinalPage(t *testing.T) {
	for _, test := range []struct {
		name     string
		maxPages int
		next     bool
	}{
		{"one-page-lower-bound", 1, true},
		{"five-page-lower-bound", 5, true},
		{"full-last-page-is-complete", 1, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
					return
				}
				if r.URL.Query().Get("per_page") == "1" {
					activityHead(w)
					return
				}
				page, _ := strconv.Atoi(r.URL.Query().Get("page"))
				if test.next {
					activityNext(w, r, page+1)
				}
				var entries []string
				for i := range 100 {
					entries = append(entries, activityCommitJSON((page-1)*100+i+1, "2026-09-08T11:00:00Z"))
				}
				_, _ = fmt.Fprint(w, "["+strings.Join(entries, ",")+"]")
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, test.maxPages)
			if err != nil || got.Commits != 100*test.maxPages || got.Complete == test.next || requests != 2+test.maxPages {
				t.Fatalf("activity=%+v requests=%d error=%v", got, requests, err)
			}
		})
	}
}

func TestFetchActivityIdleEmptyAndUntrustedConflicts(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		head       string
		wantError  bool
		wantLatest bool
	}{
		{"idle", 200, "[" + activityCommitJSON(1, "2025-01-01T00:00:00Z") + "]", false, true},
		{"confirmed-empty", 409, `{"message":"Git Repository is empty."}`, false, false},
		{"other-conflict", 409, `{"message":"Conflict"}`, true, false},
		{"invalid-conflict-json", 409, `not json`, true, false},
		{"unexplained-empty-list", 200, `[]`, true, false},
		{"unexplained-null", 200, `null`, true, false},
		{"known-commit-missing-from-window", 200, "[" + activityCommitJSON(1, "2026-09-08T11:00:00Z") + "]", true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
					return
				}
				if r.URL.Query().Get("per_page") == "1" {
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.head)
					return
				}
				_, _ = fmt.Fprint(w, "[]")
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, 1)
			if (err != nil) != test.wantError {
				t.Fatalf("activity=%+v error=%v", got, err)
			}
			if !test.wantError && (!got.Complete || got.Commits != 0 || got.ActiveDays != 0 || len(got.Daily) != 30 || (got.LatestCommitAt != nil) != test.wantLatest) {
				t.Fatalf("activity=%+v", got)
			}
			if test.status == 409 && requests != 2 {
				t.Fatal("conflict attempted more history")
			}
		})
	}
}

func TestFetchActivityRejectsIdentityAndVisibilityProblems(t *testing.T) {
	for _, payload := range []string{
		`{"id":43,"full_name":"owner/repo","default_branch":"main","private":false,"fork":false}`,
		`{"id":42,"full_name":"owner/repo","default_branch":"main","private":true,"fork":false}`,
		`{"id":42,"full_name":"owner/repo","default_branch":"main","fork":false}`,
		`{"id":42,"full_name":"owner/repo","default_branch":"main","private":false}`,
		`{"id":42,"full_name":"owner/repo","private":false,"fork":false}`,
		`{"id":42,"full_name":"owner/..","default_branch":"main","private":false,"fork":false}`,
		`{"id":42,"full_name":"owner/repo?x=1","default_branch":"main","private":false,"fork":false}`,
		`{"id":42,"full_name":"owner/repo","default_branch":" main ","private":false,"fork":false}`,
	} {
		t.Run(payload, func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				_, _ = fmt.Fprint(w, payload)
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, 1)
			if err == nil || got.RepositoryID != 0 || requests != 1 {
				t.Fatalf("identity failure became an observation: %+v, %v requests=%d", got, err, requests)
			}
		})
	}
}

func TestFetchActivityRejectsInvalidCommitData(t *testing.T) {
	for name, body := range map[string]string{
		"null":                  "null",
		"missing-sha":           `[{"commit":{"committer":{"date":"2026-09-08T11:00:00Z"}}}]`,
		"bad-sha":               `[{"sha":"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx","commit":{"committer":{"date":"2026-09-08T11:00:00Z"}}}]`,
		"missing-committer":     `[{"sha":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","commit":{"author":{"date":"2026-09-08T11:00:00Z"}}}]`,
		"malformed-date":        "[" + activityCommitJSON(1, "yesterday") + "]",
		"before-window":         "[" + activityCommitJSON(1, "2026-08-09T23:59:59Z") + "]",
		"future":                "[" + activityCommitJSON(1, "2026-09-08T12:00:01Z") + "]",
		"conflicting-duplicate": "[" + activityCommitJSON(1, "2026-09-08T10:00:00Z") + "," + activityCommitJSON(1, "2026-09-08T11:00:00Z") + "]",
	} {
		t.Run(name, func(t *testing.T) {
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
				} else if r.URL.Query().Get("per_page") == "1" {
					activityHead(w)
				} else {
					_, _ = fmt.Fprint(w, body)
				}
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, 1)
			if err == nil || got.RepositoryID != 0 {
				t.Fatalf("bad commit produced cache: %+v error=%v", got, err)
			}
		})
	}
}

func TestFetchActivityNonMonotonicCommitterDatesDoNotStopHistory(t *testing.T) {
	client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repositories/42" {
			activityMetadata(w)
		} else if r.URL.Query().Get("per_page") == "1" {
			_, _ = fmt.Fprintf(w, "[%s]", activityCommitJSON(1, "2026-08-01T00:00:00Z"))
		} else if r.URL.Query().Get("page") == "1" {
			activityNext(w, r, 2)
			_, _ = fmt.Fprintf(w, "[%s]", activityCommitJSON(2, "2026-08-11T00:00:00Z"))
		} else {
			_, _ = fmt.Fprintf(w, "[%s]", activityCommitJSON(3, "2026-09-07T00:00:00Z"))
		}
	}, nil)
	got, err := client.FetchActivity(context.Background(), 42, activityNow, 2)
	if err != nil || got.Commits != 2 || !got.Complete || got.LatestCommitAt == nil || got.LatestCommitAt.Format(time.DateOnly) != "2026-08-01" {
		t.Fatalf("nonmonotonic graph: %+v error=%v", got, err)
	}
}

func TestFetchActivityRequestFailureDiscardsPartialObservation(t *testing.T) {
	for _, test := range []struct {
		status int
		body   string
		code   ErrorCode
	}{
		{403, `{"message":"Resource not accessible"}`, CodeForbidden},
		{429, `{"message":"secondary rate limit"}`, CodeSecondaryRateLimit},
		{404, `{"message":"Not Found"}`, CodeNotFoundOrPrivate},
		{500, `{"message":"Unavailable"}`, CodeUpstream},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
				} else if r.URL.Query().Get("per_page") == "1" {
					activityHead(w)
				} else if r.URL.Query().Get("page") == "1" {
					activityNext(w, r, 2)
					activityHead(w)
				} else {
					w.WriteHeader(test.status)
					_, _ = fmt.Fprint(w, test.body)
				}
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, 2)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.code || got.RepositoryID != 0 || got.Commits != 0 {
				t.Fatalf("failure replaced reliable cache: %+v error=%v", got, err)
			}
		})
	}
}

func TestFetchActivityCrossOriginPaginationAndRedirectNeverSendCredentials(t *testing.T) {
	var leaks atomic.Int64
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaks.Add(1)
		t.Error("activity contacted a different origin")
	}))
	defer other.Close()
	for _, redirect := range []bool{false, true} {
		client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/repositories/42" {
				if redirect {
					w.Header().Set("Location", other.URL+"/repositories/42")
					w.WriteHeader(http.StatusMovedPermanently)
				} else {
					activityMetadata(w)
				}
			} else if r.URL.Query().Get("per_page") == "1" {
				activityHead(w)
			} else {
				w.Header().Set("Link", "<"+other.URL+r.URL.String()+">; rel=\"next\"")
				activityHead(w)
			}
		}, nil)
		got, err := client.FetchActivity(context.Background(), 42, activityNow, 2)
		if err == nil || got.RepositoryID != 0 || leaks.Load() != 0 {
			t.Fatalf("unsafe navigation: %+v error=%v", got, err)
		}
	}
}

func TestActivityPaginationValidatesPinnedScope(t *testing.T) {
	current, _ := url.Parse("https://api.github.com/repos/owner/repo/commits?sha=abc&since=2026-08-10T00%3A00%3A00Z&until=2026-09-08T12%3A00%3A00Z&per_page=100&page=1")
	canonical, _ := url.Parse("https://api.github.com/repositories/42/commits")
	for _, mutation := range []func(*url.URL){
		func(u *url.URL) { u.Host = "evil.invalid" },
		func(u *url.URL) { u.User = url.User("unexpected") },
		func(u *url.URL) { u.Fragment = "fragment" },
		func(u *url.URL) { u.Path = "/repos/other/repo/commits" },
		func(u *url.URL) { u.Path = "/repositories/43/commits" },
		func(u *url.URL) { u.RawQuery += "&sha=changed" },
		func(u *url.URL) { q := u.Query(); q.Set("sha", "changed"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Del("since"); u.RawQuery = q.Encode() },
		func(u *url.URL) { q := u.Query(); q.Set("page", "3"); u.RawQuery = q.Encode() },
	} {
		target := *current
		q := target.Query()
		q.Set("page", "2")
		target.RawQuery = q.Encode()
		mutation(&target)
		if _, err := activityHasNext([]string{"<" + target.String() + ">; rel=\"next\""}, current, canonical, 1); err == nil {
			t.Errorf("accepted changed pagination: %s", target.String())
		}
	}
	if _, err := activityHasNext([]string{"not a Link"}, current, canonical, 1); err == nil {
		t.Fatal("accepted malformed Link")
	}
	if next, err := activityHasNext(nil, current, canonical, 1); err != nil || next {
		t.Fatal("missing next should be complete")
	}
}

func TestActivityPaginationAcceptsObservedGitHubCanonicalLink(t *testing.T) {
	// Actual Link header returned by the official GitHub API on 2026-09-08
	// for this owner/name request. Both next and last use the permanent ID.
	current, _ := url.Parse("https://api.github.com/repos/openai/codex/commits?sha=main&since=2026-08-10T00%3A00%3A00Z&until=2026-09-08T12%3A00%3A00Z&per_page=1&page=1")
	canonical, _ := url.Parse("https://api.github.com/repositories/965415649/commits")
	link := `<https://api.github.com/repositories/965415649/commits?sha=main&since=2026-08-10T00%3A00%3A00Z&until=2026-09-08T12%3A00%3A00Z&per_page=1&page=2>; rel="next", <https://api.github.com/repositories/965415649/commits?sha=main&since=2026-08-10T00%3A00%3A00Z&until=2026-09-08T12%3A00%3A00Z&per_page=1&page=1364>; rel="last"`
	if next, err := activityHasNext([]string{link}, current, canonical, 1); err != nil || !next {
		t.Fatalf("rejected GitHub's canonical pagination: next=%v error=%v", next, err)
	}
	for _, invalid := range []string{
		strings.ReplaceAll(link, "/965415649/", "/965415650/"),
		strings.ReplaceAll(link, "sha=main", "sha=other"),
		strings.ReplaceAll(link, "api.github.com", "evil.invalid"),
		strings.ReplaceAll(link, "/965415649/", "/%39%36%35%34%31%35%36%34%39/"),
	} {
		if _, err := activityHasNext([]string{invalid}, current, canonical, 1); err == nil {
			t.Error("canonical pagination bypassed identity, origin or query checks")
		}
	}
}

func TestFetchActivityCanonicalPaginationRemainsBoundedAndSelfConstructed(t *testing.T) {
	for _, maxPages := range []int{1, 2} {
		t.Run(strconv.Itoa(maxPages), func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
					return
				}
				if r.URL.Path != "/repos/new-owner/renamed-repo/commits" {
					t.Error("client followed Link instead of constructing its own request")
				}
				if r.URL.Query().Get("per_page") == "1" {
					activityHead(w)
					return
				}
				if r.URL.Query().Get("page") == "1" {
					canonical := *r.URL
					canonical.Scheme, canonical.Host = "http", r.Host
					canonical.Path = "/repositories/42/commits"
					q := canonical.Query()
					q.Set("page", "2")
					canonical.RawQuery = q.Encode()
					w.Header().Set("Link", "<"+canonical.String()+">; rel=\"next\", <"+canonical.String()+">; rel=\"last\"")
					activityHead(w)
				} else {
					_, _ = fmt.Fprintf(w, "[%s]", activityCommitJSON(2, "2026-09-07T11:00:00Z"))
				}
			}, nil)
			got, err := client.FetchActivity(context.Background(), 42, activityNow, maxPages)
			if err != nil || got.Commits != maxPages || got.Complete != (maxPages == 2) || requests != 2+maxPages {
				t.Fatalf("canonical pagination result=%+v requests=%d error=%v", got, requests, err)
			}
		})
	}
}

func TestActivityCanonicalPaginationKeepsConfiguredAPIPrefix(t *testing.T) {
	current, _ := url.Parse("https://github.example/api/v3/repos/owner/repo/commits?sha=abc&per_page=100&page=1")
	canonical, _ := url.Parse("https://github.example/api/v3/repositories/42/commits")
	link := `<https://github.example/api/v3/repositories/42/commits?sha=abc&per_page=100&page=2>; rel="next"`
	if next, err := activityHasNext([]string{link}, current, canonical, 1); err != nil || !next {
		t.Fatalf("configured API prefix rejected: %v", err)
	}
	if _, err := activityHasNext([]string{strings.ReplaceAll(link, "/api/v3/", "/")}, current, canonical, 1); err == nil {
		t.Fatal("canonical route escaped the configured API prefix")
	}
}

func TestFetchActivityArgumentsMakeNoRequests(t *testing.T) {
	client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid arguments made a request") }, nil)
	for _, test := range []struct {
		id    int64
		at    time.Time
		pages int
	}{{0, activityNow, 1}, {42, activityNow, 0}, {42, activityNow, 6}, {42, time.Time{}, 1}, {42, activityNow.Add(time.Minute), 1}} {
		if _, err := client.FetchActivity(context.Background(), test.id, test.at, test.pages); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
}

func TestDecodeActivityCommitsRejectsOversizeAndPreservesSHA256(t *testing.T) {
	var entries []json.RawMessage
	for i := range 101 {
		entries = append(entries, json.RawMessage(activityCommitJSON(i+1, "2026-09-08T11:00:00Z")))
	}
	body, _ := json.Marshal(entries)
	if _, err := decodeActivityCommits(body, 100); err == nil {
		t.Fatal("accepted more commits than requested")
	}
	body = []byte(`[{"sha":"` + strings.Repeat("a", 64) + `","commit":{"committer":{"date":"2026-09-08T11:00:00Z"}}}]`)
	if _, err := decodeActivityCommits(body, 1); err != nil {
		t.Fatal(err)
	}
}

func TestFetchActivityProtectsCoreQuotaBetweenRepositoryRequests(t *testing.T) {
	for _, remaining := range []int{99, 0} {
		t.Run(strconv.Itoa(remaining), func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				w.Header().Set("X-RateLimit-Limit", "5000")
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(activityNow.Add(time.Hour).Unix(), 10))
				activityMetadata(w)
			}, func(options *ClientOptions) {
				options.Sleep = func(context.Context, time.Duration) error {
					t.Error("optional activity must stop instead of sleeping until quota reset")
					return errors.New("unexpected sleep")
				}
			})
			// 100 remaining permits one request; its response can take us below
			// the reserve before the next step of this same repository.
			client.recordRate(RateLimit{Resource: ResourceCore, Limit: 5000, Remaining: 100, Reset: activityNow.Add(time.Hour)})
			got, err := client.FetchActivity(context.Background(), 42, activityNow, 1)
			var apiErr *APIError
			if requests != 1 || got.RepositoryID != 0 || !errors.As(err, &apiErr) || apiErr.Code != CodePrimaryRateLimit || apiErr.StatusCode != 429 {
				t.Fatalf("quota not reserved: requests=%d result=%+v error=%v", requests, got, err)
			}
		})
	}
}

func TestActivityQuotaGuardIgnoresUnknownOrExpiredLimits(t *testing.T) {
	for name, rate := range map[string]RateLimit{
		"unknown-limit":   {Resource: ResourceCore, Remaining: 99, Reset: activityNow.Add(time.Hour)},
		"unknown-balance": {Resource: ResourceCore, Limit: 5000, Remaining: -1, Reset: activityNow.Add(time.Hour)},
		"expired-reset":   {Resource: ResourceCore, Limit: 5000, Remaining: 0, Reset: activityNow.Add(-time.Second)},
	} {
		t.Run(name, func(t *testing.T) {
			requests := 0
			client := activityTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.URL.Path == "/repositories/42" {
					activityMetadata(w)
				} else {
					activityHead(w)
				}
			}, nil)
			client.recordRate(rate)
			if _, err := client.FetchActivity(context.Background(), 42, activityNow, 1); err != nil || requests != 3 {
				t.Fatalf("unknown or stale rate blocked activity: %d, %v", requests, err)
			}
		})
	}
}

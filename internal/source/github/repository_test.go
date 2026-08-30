package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func repositoryFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "github", "repository.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func githubTestClient(t *testing.T, server *httptest.Server, mutate func(*ClientOptions)) *Client {
	t.Helper()
	options := ClientOptions{
		BaseURL:          server.URL + "/",
		Token:            "test-token",
		APIVersion:       "2026-03-10",
		UserAgent:        "github-radar-test",
		HTTPClient:       server.Client(),
		SecondaryBackoff: time.Minute,
		ServerBackoff:    time.Millisecond,
	}
	if mutate != nil {
		mutate(&options)
	}
	client, err := NewClient(options)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func TestFetchRepository200AuthenticationAndETag(t *testing.T) {
	fixture := repositoryFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer test-token"; got != want {
			t.Errorf("Authorization = %q, want %q", got, want)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2026-03-10" {
			t.Errorf("API version = %q", got)
		}
		if got := r.Header.Get("If-None-Match"); got != `W/"old"` {
			t.Errorf("If-None-Match = %q", got)
		}
		w.Header().Set("ETag", `W/"new"`)
		w.Header().Set("X-RateLimit-Resource", "core")
		w.Header().Set("X-RateLimit-Remaining", "4999")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)

	result, err := client.FetchRepository(context.Background(), RepositoryRequest{
		FullName:   "octocat/Hello-World",
		ExpectedID: 1296269,
		ETag:       `W/"old"`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Repository.AbsoluteStars == nil || *result.Repository.AbsoluteStars != 123 {
		t.Fatalf("absolute stars = %v", result.Repository.AbsoluteStars)
	}
	if result.ETag != `W/"new"` || result.RateLimit.Remaining != 4999 {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchRepositoryNotModified(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") != `W/"same"` {
			t.Error("conditional request omitted ETag")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo", ETag: `W/"same"`})
	if err != nil {
		t.Fatal(err)
	}
	if !result.NotModified || result.HTTPStatus != http.StatusNotModified || result.ETag != `W/"same"` {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchRepositoryByPermanentID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/repositories/1296269"; got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
		_, _ = w.Write(repositoryFixture(t))
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.FetchRepositoryByID(context.Background(), 1296269, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Repository.ID != 1296269 {
		t.Fatalf("repository = %+v", result.Repository)
	}
}

func TestFetchRepositoryPermanentRedirect(t *testing.T) {
	fixture := repositoryFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/old/name":
			w.Header().Set("Location", "/repos/octocat/Hello-World")
			w.WriteHeader(http.StatusMovedPermanently)
		case "/repos/octocat/Hello-World":
			_, _ = w.Write(fixture)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "old/name", ExpectedID: 1296269})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Redirected || result.RedirectedFrom != "old/name" || result.Repository.FullName != "octocat/Hello-World" {
		t.Fatalf("result = %+v", result)
	}
}

func TestFetchRepositoryStatusErrors(t *testing.T) {
	tests := []struct {
		status int
		body   string
		code   ErrorCode
	}{
		{http.StatusForbidden, `{"message":"Resource not accessible"}`, CodeForbidden},
		{http.StatusNotFound, `{"message":"Not Found"}`, CodeNotFoundOrPrivate},
		{http.StatusTooManyRequests, `{"message":"rate limited"}`, CodeSecondaryRateLimit},
		{http.StatusInternalServerError, `{"message":"oops"}`, CodeUpstream},
		{http.StatusServiceUnavailable, `{"message":"down"}`, CodeUpstream},
	}
	for _, test := range tests {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			client := githubTestClient(t, server, nil)
			_, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo"})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != test.code {
				t.Fatalf("error = %#v, want API code %s", err, test.code)
			}
		})
	}
}

func TestFetchRepositoryIDMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(repositoryFixture(t))
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	result, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "octocat/Hello-World", ExpectedID: 99})
	var mismatch *RepositoryIDMismatchError
	if !errors.As(err, &mismatch) || result.Repository.ID != 1296269 {
		t.Fatalf("result = %+v, error = %#v", result, err)
	}
}

func TestFetchRepositoryTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write(repositoryFixture(t))
	}))
	defer server.Close()
	client := githubTestClient(t, server, func(options *ClientOptions) { options.Timeout = 10 * time.Millisecond })
	_, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo"})
	var transport *TransportError
	if !errors.As(err, &transport) || transport.Code != CodeTimeout {
		t.Fatalf("error = %#v, want timeout", err)
	}
}

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func (f *fakeClock) Now() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *fakeClock) Sleep(ctx context.Context, duration time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	f.mu.Lock()
	f.sleeps = append(f.sleeps, duration)
	f.now = f.now.Add(duration)
	f.mu.Unlock()
	return nil
}

func TestRetryAfterAndPrimaryReset(t *testing.T) {
	tests := []struct {
		name      string
		headers   map[string]string
		status    int
		body      string
		wantSleep time.Duration
	}{
		{name: "retry-after", headers: map[string]string{"Retry-After": "2"}, status: http.StatusTooManyRequests, body: `{"message":"secondary rate limit"}`, wantSleep: 2 * time.Second},
		{name: "primary-reset", headers: map[string]string{"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": "1788051605"}, status: http.StatusForbidden, body: `{"message":"API rate limit exceeded"}`, wantSleep: 6 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := &fakeClock{now: time.Unix(1788051600, 0)}
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if calls.Add(1) == 1 {
					for key, value := range test.headers {
						w.Header().Set(key, value)
					}
					w.WriteHeader(test.status)
					_, _ = w.Write([]byte(test.body))
					return
				}
				_, _ = w.Write(repositoryFixture(t))
			}))
			defer server.Close()
			client := githubTestClient(t, server, func(options *ClientOptions) {
				options.Now = clock.Now
				options.Sleep = clock.Sleep
				options.MaxRetries = 1
			})
			if _, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo"}); err != nil {
				t.Fatal(err)
			}
			if len(clock.sleeps) != 1 || clock.sleeps[0] != test.wantSleep {
				t.Fatalf("sleeps = %v, want [%s]", clock.sleeps, test.wantSleep)
			}
		})
	}
}

func TestSecondaryAndServerBackoff(t *testing.T) {
	t.Run("secondary without retry-after waits at least a minute", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"message":"You have exceeded a secondary rate limit"}`))
				return
			}
			_, _ = w.Write(repositoryFixture(t))
		}))
		defer server.Close()
		client := githubTestClient(t, server, func(options *ClientOptions) {
			options.Now = clock.Now
			options.Sleep = clock.Sleep
			options.MaxRetries = 1
		})
		if _, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo"}); err != nil {
			t.Fatal(err)
		}
		if len(clock.sleeps) != 1 || clock.sleeps[0] != time.Minute {
			t.Fatalf("sleeps = %v, want [1m]", clock.sleeps)
		}
	})

	t.Run("server errors use exponential backoff", func(t *testing.T) {
		clock := &fakeClock{now: time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)}
		var calls atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) <= 2 {
				w.WriteHeader(http.StatusServiceUnavailable)
				_, _ = w.Write([]byte(`{"message":"try again"}`))
				return
			}
			_, _ = w.Write(repositoryFixture(t))
		}))
		defer server.Close()
		client := githubTestClient(t, server, func(options *ClientOptions) {
			options.Now = clock.Now
			options.Sleep = clock.Sleep
			options.ServerBackoff = 10 * time.Millisecond
			options.MaxRetries = 2
		})
		if _, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: "owner/repo"}); err != nil {
			t.Fatal(err)
		}
		if len(clock.sleeps) != 2 || clock.sleeps[0] != 10*time.Millisecond || clock.sleeps[1] != 20*time.Millisecond {
			t.Fatalf("sleeps = %v", clock.sleeps)
		}
	})
}

func TestClientSerializesRequests(t *testing.T) {
	var active atomic.Int32
	var maximum atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		current := active.Add(1)
		for {
			old := maximum.Load()
			if current <= old || maximum.CompareAndSwap(old, current) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		_, _ = w.Write(repositoryFixture(t))
	}))
	defer server.Close()
	client := githubTestClient(t, server, nil)
	var wait sync.WaitGroup
	for index := 0; index < 4; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			_, err := client.FetchRepository(context.Background(), RepositoryRequest{FullName: fmt.Sprintf("owner/repo%d", index)})
			if err != nil {
				t.Errorf("FetchRepository() error = %v", err)
			}
		}(index)
	}
	wait.Wait()
	if got := maximum.Load(); got != 1 {
		t.Fatalf("maximum concurrent requests = %d, want 1", got)
	}
}

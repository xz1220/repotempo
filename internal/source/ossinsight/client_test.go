package ossinsight

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xz1220/repotempo/internal/config"
)

func ossFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "ossinsight", "window.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestFetchFourWindowsAndKeepWindowStarsSeparate(t *testing.T) {
	fixture := ossFixture(t)
	periods := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		periods[r.URL.Query().Get("period")]++
		if got := r.URL.Query().Get("language"); got != "C++" {
			t.Errorf("language = %q, want C++", got)
		}
		if !strings.Contains(r.URL.RawQuery, "C%2B%2B") {
			t.Errorf("language was not URL encoded: %s", r.URL.RawQuery)
		}
		_, _ = w.Write(fixture)
	}))
	defer server.Close()
	now := time.Date(2026, 8, 30, 1, 2, 3, 0, time.UTC)
	client, err := NewClient(ClientOptions{BaseURL: server.URL + "/v1/trends/repos/", HTTPClient: server.Client(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	windows := []config.OSSWindow{
		{Name: "today", Period: "past_24_hours"},
		{Name: "week", Period: "past_week"},
		{Name: "month", Period: "past_month"},
		{Name: "three-months", Period: "past_3_months"},
	}
	result, err := client.FetchWindows(context.Background(), windows, "C++")
	if err != nil {
		t.Fatal(err)
	}
	if len(periods) != 4 || len(result.Repositories) != 1 || len(result.Repositories[0].Signals) != 4 {
		t.Fatalf("periods = %v result = %+v", periods, result)
	}
	repository := result.Repositories[0]
	if repository.Repository.AbsoluteStars != nil {
		t.Fatalf("OSS window stars leaked into absolute stars: %v", repository.Repository.AbsoluteStars)
	}
	signal := repository.Signals["today"]
	if signal.WindowStars == nil || *signal.WindowStars != 34 || signal.PullRequests != nil || signal.Rank != 1 {
		t.Fatalf("signal = %+v", signal)
	}
	if len(result.Columns) != 11 || result.RowsByWindow["today"] != 1 {
		t.Fatalf("columns/rows = %v / %v", result.Columns, result.RowsByWindow)
	}
	if got := signal.CapturedAt; !got.Equal(now) {
		t.Fatalf("captured at = %s, want %s", got, now)
	}
}

func TestFetchWindowInvalidAndDuplicateRowsBecomeWarnings(t *testing.T) {
	body := `{
      "data": {"columns": [{"col":"repo_id"}], "rows": [
        {"repo_id":"1","repo_name":"owner/repo","stars":"4"},
        {"repo_id":"1","repo_name":"owner/repo","stars":"5"},
        {"repo_id":"bad","repo_name":"broken","stars":"x"},
        {"repo_id":"2","repo_name":"owner/two","stars":"not-a-number"}
      ]}}
    `
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(body)) }))
	defer server.Close()
	client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.FetchWindows(context.Background(), []config.OSSWindow{{Name: "today", Period: "past_24_hours"}}, "All")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Repositories) != 2 {
		t.Fatalf("repositories = %d, want 2", len(result.Repositories))
	}
	if len(result.Warnings) < 3 {
		t.Fatalf("warnings = %+v", result.Warnings)
	}
	if result.Repositories[1].Signals["today"].WindowStars != nil {
		t.Fatal("invalid numeric field should be missing, not zero")
	}
}

func TestFetchWindowHTTPErrorAndTimeout(t *testing.T) {
	t.Run("HTTP error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("temporarily unavailable"))
		}))
		defer server.Close()
		client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client()})
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.FetchWindows(context.Background(), []config.OSSWindow{{Name: "today", Period: "past_24_hours"}}, "All")
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("error = %#v", err)
		}
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			_, _ = w.Write(ossFixture(t))
		}))
		defer server.Close()
		client, err := NewClient(ClientOptions{BaseURL: server.URL, HTTPClient: server.Client(), Timeout: 5 * time.Millisecond})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := client.FetchWindows(context.Background(), []config.OSSWindow{{Name: "today", Period: "past_24_hours"}}, "All"); err == nil {
			t.Fatal("FetchWindows() error = nil, want timeout")
		}
	})
}

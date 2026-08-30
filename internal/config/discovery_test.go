package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadDiscoveryExample(t *testing.T) {
	config, err := LoadDiscovery(filepath.Join("..", "..", "config", "discovery.example.yaml"))
	if err != nil {
		t.Fatalf("LoadDiscovery() error = %v", err)
	}
	if got, want := len(config.OSSInsight.Windows), 4; got != want {
		t.Fatalf("OSS windows = %d, want %d", got, want)
	}
	if got, want := len(config.Profiles), 6; got != want {
		t.Fatalf("profiles = %d, want %d", got, want)
	}
	if config.Profiles[0].Queries[0].Stars.Min == nil || *config.Profiles[0].Queries[0].Stars.Min != 20000 {
		t.Fatal("global star threshold was not loaded from YAML")
	}
	if config.Profiles[4].IsEnabled() {
		t.Fatal("monthly coverage must be explicit opt-in in the single-host example")
	}
	if got := config.GitHub.SearchInterval.Value(); got != 2*time.Second {
		t.Fatalf("search interval = %s, want 2s", got)
	}
	benchmark := config.Profiles[len(config.Profiles)-1]
	for _, query := range benchmark.Queries {
		if !strings.Contains(query.Text, " user:") {
			t.Fatalf("benchmark query is not owner-scoped: %q", query.Text)
		}
	}
}

func TestDecodeDiscoveryRejectsUnknownFields(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "discovery.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw, []byte("unknown_field: true\n")...)
	if _, err := DecodeDiscovery(strings.NewReader(string(raw))); err == nil || !strings.Contains(err.Error(), "field unknown_field not found") {
		t.Fatalf("DecodeDiscovery() error = %v, want unknown field", err)
	}
}

func TestDecodeDiscoveryCapsRetries(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "discovery.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(string(raw), "max_retries: 3", "max_retries: 6", 1)
	if _, err := DecodeDiscovery(strings.NewReader(invalid)); err == nil || !strings.Contains(err.Error(), "must not exceed 5") {
		t.Fatalf("DecodeDiscovery() error = %v, want retry cap", err)
	}
}

func TestDecodeDiscoveryRequiresPositiveCoreInterval(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "config", "discovery.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	invalid := strings.Replace(string(raw), "core_interval: 100ms", "core_interval: 0s", 1)
	if _, err := DecodeDiscovery(strings.NewReader(invalid)); err == nil || !strings.Contains(err.Error(), "core_interval must be positive") {
		t.Fatalf("DecodeDiscovery() error = %v, want positive core interval", err)
	}
}

func TestDateRangeResolveRelative(t *testing.T) {
	now := time.Date(2026, 8, 30, 13, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	from, to, err := (DateRange{SinceDays: 30}).Resolve(now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := from.Format("2006-01-02"), "2026-07-31"; got != want {
		t.Fatalf("from = %s, want %s", got, want)
	}
	if to != nil {
		t.Fatalf("to = %v, want nil", to)
	}
}

func TestSearchProfileDue(t *testing.T) {
	profile := SearchProfile{Schedule: "weekly"}
	now := time.Date(2026, 8, 30, 9, 0, 0, 0, time.UTC)
	last := now.AddDate(0, 0, -7)
	if !profile.Due(now, &last) {
		t.Fatal("weekly profile should be due in a later ISO week")
	}
	manual := SearchProfile{Schedule: "manual"}
	if manual.Due(now, &last) {
		t.Fatal("manual profile must not be automatically due")
	}
}

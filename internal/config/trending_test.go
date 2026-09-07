package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestTrendingConfigurationIsPrimaryAndBounded(t *testing.T) {
	cfg, err := LoadDiscovery(filepath.Join("..", "..", "config", "discovery.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Trending.IsEnabled() || len(cfg.Trending.Periods) != 3 || cfg.OSSInsight.IsEnabled() {
		t.Fatal("expected Trending primary with all three periods and OSS disabled")
	}
	if (Trending{}).IsEnabled() {
		t.Fatal("omitted Trending must remain compatible with old configurations")
	}
	for _, test := range []struct {
		name string
		edit func(*Trending)
	}{
		{"missing periods", func(v *Trending) { v.Periods = nil }},
		{"duplicate periods", func(v *Trending) { v.Periods = []string{"daily", "daily"} }},
		{"invalid period", func(v *Trending) { v.Periods = []string{"yearly"} }},
		{"aggressive interval", func(v *Trending) { v.Interval = Duration(time.Millisecond) }},
		{"missing timeout", func(v *Trending) { v.Timeout = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			invalid := cfg
			test.edit(&invalid.Trending)
			if invalid.Validate() == nil {
				t.Fatal("invalid Trending configuration accepted")
			}
		})
	}
}

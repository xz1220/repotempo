package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckRuntimeDoesNotIncludeTokenValue(t *testing.T) {
	directory := t.TempDir()
	discovery := filepath.Join(directory, "discovery.yaml")
	topics := filepath.Join(directory, "topics.yaml")
	if err := os.WriteFile(discovery, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(topics, []byte("version: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings := Settings{
		DatabasePath:    filepath.Join(directory, "radar.db"),
		DiscoveryConfig: discovery,
		TopicsConfig:    topics,
		GitHubToken:     "test-token-not-a-secret",
	}
	report := CheckRuntime(settings)
	for _, check := range report.Checks {
		if check.Message == settings.GitHubToken {
			t.Fatal("doctor exposed token value")
		}
	}
}

func TestCheckRuntimeFailsMissingConfigs(t *testing.T) {
	directory := t.TempDir()
	report := CheckRuntime(Settings{
		DatabasePath:    filepath.Join(directory, "radar.db"),
		DiscoveryConfig: filepath.Join(directory, "missing-discovery.yaml"),
		TopicsConfig:    filepath.Join(directory, "missing-topics.yaml"),
	})
	if report.Healthy {
		t.Fatal("expected missing configuration to fail doctor")
	}
}

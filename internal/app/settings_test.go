package app

import (
	"strings"
	"testing"
)

func TestLoadSettingsDoesNotExposeToken(t *testing.T) {
	t.Setenv("GITHUB_RADAR_GITHUB_TOKEN", "test-token-not-a-secret")
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.DiagnosticFields()["github_token_configured"].(bool) {
		t.Fatal("expected configured token marker")
	}
	for _, value := range settings.DiagnosticFields() {
		if value == settings.GitHubToken {
			t.Fatal("diagnostic fields exposed the token")
		}
	}
}

func TestLoadSettingsRejectsInvalidRetention(t *testing.T) {
	t.Setenv("GITHUB_RADAR_EXPORT_RETENTION_DAYS", "0")
	if _, err := LoadSettings(); err == nil {
		t.Fatal("expected invalid retention error")
	}
}

func TestDefaultSettingsNeverSelectExampleFiles(t *testing.T) {
	t.Setenv("GITHUB_RADAR_DISCOVERY_CONFIG", "")
	t.Setenv("GITHUB_RADAR_TOPICS_CONFIG", "")
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(settings.DiscoveryConfig, "example") || strings.Contains(settings.TopicsConfig, "example") {
		t.Fatalf("unsafe defaults: %#v", settings.DiagnosticFields())
	}
}

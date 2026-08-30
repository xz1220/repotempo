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

func TestLoadSettingsDefaultsToEnglishLocale(t *testing.T) {
	t.Setenv("GITHUB_RADAR_LOCALE", "")
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Locale != "en" {
		t.Fatalf("locale = %q, want en", settings.Locale)
	}
	if settings.DiagnosticFields()["locale"] != "en" {
		t.Fatalf("diagnostic locale = %#v, want en", settings.DiagnosticFields()["locale"])
	}
}

func TestLoadSettingsAcceptsChineseLocale(t *testing.T) {
	t.Setenv("GITHUB_RADAR_LOCALE", "zh-CN")
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if settings.Locale != "zh-CN" {
		t.Fatalf("locale = %q, want zh-CN", settings.Locale)
	}
}

func TestLoadSettingsRejectsInvalidLocale(t *testing.T) {
	t.Setenv("GITHUB_RADAR_LOCALE", "zh")
	_, err := LoadSettings()
	if err == nil || !strings.Contains(err.Error(), "GITHUB_RADAR_LOCALE must be en or zh-CN") {
		t.Fatalf("error = %v, want explicit locale validation error", err)
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

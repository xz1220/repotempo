package app

import (
	"strings"
	"testing"
)

func TestLoadSettingsDoesNotExposeToken(t *testing.T) {
	t.Setenv("GITHUB_RADAR_GITHUB_TOKEN", "test-token-not-a-secret")
	t.Setenv("GITHUB_RADAR_WEB_WRITE_TOKEN", "test-management-token-not-a-secret")
	settings, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if !settings.DiagnosticFields()["github_token_configured"].(bool) {
		t.Fatal("expected configured token marker")
	}
	for _, value := range settings.DiagnosticFields() {
		if value == settings.GitHubToken || value == settings.WebWriteToken {
			t.Fatal("diagnostic fields exposed the token")
		}
	}
}

func TestLoadSettingsRejectsShortWebWriteToken(t *testing.T) {
	t.Setenv("GITHUB_RADAR_WEB_WRITE_TOKEN", "short")
	if _, err := LoadSettings(); err == nil {
		t.Fatal("short management token accepted")
	}
}

func TestOnlyLoopbackListenersAllowLocalWrites(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8878", "[::1]:8878", "localhost:8878"} {
		if !isLoopbackListenAddress(address) {
			t.Fatalf("local listener rejected: %s", address)
		}
	}
	for _, address := range []string{":8878", "0.0.0.0:8878", "[::]:8878", "192.0.2.1:8878", "localhost.evil.test:8878"} {
		if isLoopbackListenAddress(address) {
			t.Fatalf("public listener allowed: %s", address)
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

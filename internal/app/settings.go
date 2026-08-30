package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	defaultDatabasePath    = "github-radar.db"
	defaultExportDirectory = "exports"
	defaultBackupDirectory = "backups"
	defaultDiscoveryConfig = "config/discovery.yaml"
	defaultTopicsConfig    = "config/topics.yaml"
	defaultListenAddress   = "127.0.0.1:8787"
	defaultTimezone        = "Asia/Shanghai"
	defaultLocale          = "en"
)

// Settings contains runtime configuration. Secret values are deliberately
// excluded from String and DiagnosticFields.
type Settings struct {
	DatabasePath       string
	ExportDirectory    string
	BackupDirectory    string
	DiscoveryConfig    string
	TopicsConfig       string
	ListenAddress      string
	Timezone           string
	Locale             string
	LogFormat          string
	LogLevel           string
	GitHubToken        string
	LegacyDatabasePath string
	ExportRetention    int
	BackupRetention    int
}

// LoadSettings reads environment variables without loading dotenv files or
// accepting tokens on the command line.
func LoadSettings() (Settings, error) {
	settings := Settings{
		DatabasePath:       envOr("GITHUB_RADAR_DB_PATH", defaultDatabasePath),
		ExportDirectory:    envOr("GITHUB_RADAR_EXPORT_DIR", defaultExportDirectory),
		BackupDirectory:    envOr("GITHUB_RADAR_BACKUP_DIR", defaultBackupDirectory),
		DiscoveryConfig:    envOr("GITHUB_RADAR_DISCOVERY_CONFIG", defaultDiscoveryConfig),
		TopicsConfig:       envOr("GITHUB_RADAR_TOPICS_CONFIG", defaultTopicsConfig),
		ListenAddress:      envOr("GITHUB_RADAR_LISTEN_ADDR", defaultListenAddress),
		Timezone:           envOr("GITHUB_RADAR_TIMEZONE", defaultTimezone),
		Locale:             envOr("GITHUB_RADAR_LOCALE", defaultLocale),
		LogFormat:          envOr("GITHUB_RADAR_LOG_FORMAT", "text"),
		LogLevel:           envOr("GITHUB_RADAR_LOG_LEVEL", "info"),
		GitHubToken:        strings.TrimSpace(os.Getenv("GITHUB_RADAR_GITHUB_TOKEN")),
		LegacyDatabasePath: strings.TrimSpace(os.Getenv("GITHUB_RADAR_LEGACY_DB_PATH")),
	}

	var err error
	settings.ExportRetention, err = positiveEnvInt("GITHUB_RADAR_EXPORT_RETENTION_DAYS", 30)
	if err != nil {
		return Settings{}, err
	}
	settings.BackupRetention, err = positiveEnvInt("GITHUB_RADAR_BACKUP_RETENTION_DAYS", 30)
	if err != nil {
		return Settings{}, err
	}
	if settings.LogFormat != "text" && settings.LogFormat != "json" {
		return Settings{}, fmt.Errorf("GITHUB_RADAR_LOG_FORMAT must be text or json")
	}
	if settings.Timezone != defaultTimezone {
		return Settings{}, fmt.Errorf("GITHUB_RADAR_TIMEZONE must be %s in v0.1.0", defaultTimezone)
	}
	if settings.Locale != "en" && settings.Locale != "zh-CN" {
		return Settings{}, fmt.Errorf("GITHUB_RADAR_LOCALE must be en or zh-CN")
	}
	return settings, nil
}

func (settings Settings) DiagnosticFields() map[string]any {
	return map[string]any{
		"database_path":           cleanPath(settings.DatabasePath),
		"export_directory":        cleanPath(settings.ExportDirectory),
		"backup_directory":        cleanPath(settings.BackupDirectory),
		"discovery_config":        cleanPath(settings.DiscoveryConfig),
		"topics_config":           cleanPath(settings.TopicsConfig),
		"listen_address":          settings.ListenAddress,
		"timezone":                settings.Timezone,
		"locale":                  settings.Locale,
		"log_format":              settings.LogFormat,
		"github_token_configured": settings.GitHubToken != "",
	}
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func positiveEnvInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return value, nil
}

func cleanPath(value string) string {
	if value == "" {
		return ""
	}
	return filepath.Clean(value)
}

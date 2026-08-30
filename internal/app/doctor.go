package app

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
)

type DoctorCheck struct {
	Name    string      `json:"name"`
	Status  CheckStatus `json:"status"`
	Message string      `json:"message"`
}

type DoctorReport struct {
	Healthy   bool          `json:"healthy"`
	Checks    []DoctorCheck `json:"checks"`
	DiskUsage *float64      `json:"disk_usage_percent,omitempty"`
}

// CheckRuntime validates local runtime prerequisites without calling external
// APIs or revealing secret values. API reachability is checked by the command
// layer using the configured clients.
func CheckRuntime(settings Settings) DoctorReport {
	report := DoctorReport{Healthy: true}
	add := func(name string, status CheckStatus, message string) {
		report.Checks = append(report.Checks, DoctorCheck{Name: name, Status: status, Message: message})
		if status == CheckFail {
			report.Healthy = false
		}
	}

	if settings.GitHubToken == "" {
		add("github_token", CheckWarn, "GitHub token is not configured; authenticated collection is unavailable")
	} else {
		add("github_token", CheckPass, "GitHub token is configured")
	}

	for _, file := range []struct {
		name string
		path string
	}{
		{name: "discovery_config", path: settings.DiscoveryConfig},
		{name: "topics_config", path: settings.TopicsConfig},
	} {
		info, err := os.Stat(file.path)
		switch {
		case err != nil:
			add(file.name, CheckFail, fmt.Sprintf("cannot read %s", cleanPath(file.path)))
		case info.IsDir():
			add(file.name, CheckFail, fmt.Sprintf("%s is a directory", cleanPath(file.path)))
		default:
			add(file.name, CheckPass, fmt.Sprintf("%s is readable", cleanPath(file.path)))
		}
	}

	databaseParent := existingParent(settings.DatabasePath)
	if databaseParent == "" {
		add("database_directory", CheckFail, "database parent directory does not exist")
		return report
	}
	if err := unix.Access(databaseParent, unix.W_OK); err != nil {
		add("database_directory", CheckFail, fmt.Sprintf("database directory is not writable: %s", cleanPath(databaseParent)))
	} else {
		add("database_directory", CheckPass, fmt.Sprintf("database directory is writable: %s", cleanPath(databaseParent)))
	}

	var filesystem unix.Statfs_t
	if err := unix.Statfs(databaseParent, &filesystem); err != nil {
		add("disk_usage", CheckWarn, "disk usage could not be measured")
		return report
	}
	if filesystem.Blocks == 0 {
		add("disk_usage", CheckWarn, "disk usage is unavailable for this filesystem")
		return report
	}
	used := float64(filesystem.Blocks-filesystem.Bavail) / float64(filesystem.Blocks) * 100
	report.DiskUsage = &used
	switch {
	case used >= 95:
		add("disk_usage", CheckFail, fmt.Sprintf("disk usage is %.1f%%; collection should stop before writes fail", used))
	case used >= 85:
		add("disk_usage", CheckWarn, fmt.Sprintf("disk usage is %.1f%%; cleanup or expansion is required", used))
	default:
		add("disk_usage", CheckPass, fmt.Sprintf("disk usage is %.1f%%", used))
	}
	return report
}

func existingParent(path string) string {
	current := filepath.Clean(filepath.Dir(path))
	for {
		info, err := os.Stat(current)
		if err == nil && info.IsDir() {
			return current
		}
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		current = parent
	}
}

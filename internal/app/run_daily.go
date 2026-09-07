package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/exporter"
	"github.com/xz1220/repotempo/internal/service/jobs"
	"github.com/xz1220/repotempo/internal/service/snapshot"
	"github.com/xz1220/repotempo/internal/source"
	"github.com/xz1220/repotempo/internal/source/github"
)

type DailyJobDetails struct {
	Runtime             DoctorReport       `json:"runtime"`
	Discovery           DiscoverReport     `json:"discovery"`
	Snapshot            snapshot.Report    `json:"snapshot"`
	Exports             []string           `json:"exports"`
	FailureRepositories []string           `json:"failure_repositories"`
	Failures            []OperationFailure `json:"failures"`
	Cleaned             []string           `json:"cleaned"`
	SearchRateRemaining *int               `json:"search_rate_remaining,omitempty"`
	CoreRateRemaining   *int               `json:"core_rate_remaining,omitempty"`
	IncompleteResults   bool               `json:"incomplete_results"`
	QuerySplitCount     int                `json:"query_split_count"`
}

func (runtime *Runtime) RunDaily(ctx context.Context, dryRun bool) (DailyReport, error) {
	if dryRun {
		return runtime.planDaily(ctx)
	}
	runtimeReport := runtime.checkRuntime(runtime.settings)
	if !runtimeReport.Healthy {
		return DailyReport{
			Fatal:    true,
			Runtime:  runtimeReport,
			Exports:  []exporter.Result{},
			Cleaned:  []string{},
			Failures: []OperationFailure{{Stage: "runtime", Message: "runtime checks failed; daily writes were not started"}},
		}, nil
	}
	execution, err := runtime.tracker().Start(ctx, "run-daily", map[string]any{"stage": "starting"})
	if err != nil {
		return DailyReport{}, err
	}
	report := DailyReport{Exports: []exporter.Result{}, Failures: []OperationFailure{}}
	report.RunID = execution.Run.RunID
	report.Runtime = runtimeReport

	discoveryReport, discoveryErr := runtime.Discover(ctx, DiscoverOptions{
		Source:        "all",
		DueOnly:       true,
		IncludeManual: true,
		IncludeLegacy: false,
	})
	report.Discovery = discoveryReport
	if discoveryErr != nil {
		report.Failures = append(report.Failures, OperationFailure{Stage: "discovery", Message: discoveryErr.Error()})
	}

	_, githubClient, _, githubErr := runtime.dependencies()
	if discoveryReport.APIBlocked {
		report.Fatal = true
		report.Failures = append(report.Failures, OperationFailure{Stage: "snapshot", Message: "GitHub API authentication or rate limit rejected requests; remaining API work deferred"})
		report.Snapshot.Date = domain.ShanghaiDate(runtime.now())
		var countErr error
		report.Snapshot.TargetCount, countErr = runtime.activeRepositoryCount(ctx)
		if countErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "snapshot-plan", Message: countErr.Error()})
		}
	} else if githubErr != nil {
		report.Fatal = true
		report.Failures = append(report.Failures, OperationFailure{Stage: "snapshot-configuration", Message: githubErr.Error()})
	} else {
		snapshotReport, snapshotErr := runtime.snapshotService(githubClient).Run(ctx, discoveryReport.OSSEvidence)
		report.Snapshot = SnapshotCommandReport{Report: snapshotReport}
		if snapshotErr != nil {
			report.Fatal = true
			report.Failures = append(report.Failures, OperationFailure{Stage: "snapshot", Message: snapshotErr.Error()})
		}
	}

	for _, target := range []struct {
		format    string
		directory string
	}{
		{format: "csv", directory: runtime.settings.ExportDirectory},
		{format: "json", directory: runtime.settings.ExportDirectory},
		{format: "sqlite", directory: runtime.settings.BackupDirectory},
	} {
		result, exportErr := runtime.dataExporter().Export(ctx, target.format, target.directory)
		if exportErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "export", Target: target.format, Message: exportErr.Error()})
			continue
		}
		report.Exports = append(report.Exports, result)
	}
	for _, retention := range []struct {
		directory string
		days      int
	}{
		{directory: runtime.settings.ExportDirectory, days: runtime.settings.ExportRetention},
		{directory: runtime.settings.BackupDirectory, days: runtime.settings.BackupRetention},
	} {
		cleaned, cleanupErr := cleanupRetention(retention.directory, retention.days, runtime.now())
		report.Cleaned = append(report.Cleaned, cleaned...)
		if cleanupErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "retention", Target: retention.directory, Message: cleanupErr.Error()})
		}
	}

	status := domain.JobSuccess
	if report.Fatal {
		status = domain.JobFailed
	} else if report.Partial() {
		status = domain.JobPartial
	}
	details := dailyDetails(report, runtime.github)
	finishErr := execution.Finish(context.WithoutCancel(ctx), jobs.Outcome{
		Status:       status,
		TargetCount:  report.Snapshot.TargetCount,
		SuccessCount: report.Snapshot.SuccessCount,
		FailureCount: report.Snapshot.FailureCount + report.Snapshot.MetadataFailureCount,
		SkippedCount: report.Snapshot.SkippedCount,
		Details:      details,
		ErrorSummary: summarizeDailyErrors(report),
	})
	if finishErr != nil {
		return report, finishErr
	}
	return report, nil
}

func (runtime *Runtime) planDaily(ctx context.Context) (DailyReport, error) {
	runtimeReport := runtime.checkRuntime(runtime.settings)
	if !runtimeReport.Healthy {
		return DailyReport{
			DryRun:   true,
			Fatal:    true,
			Runtime:  runtimeReport,
			Exports:  []exporter.Result{},
			Cleaned:  []string{},
			Failures: []OperationFailure{{Stage: "runtime", Message: "runtime checks failed; daily plan was not opened"}},
		}, nil
	}
	discoveryConfig, _, _, err := runtime.dependencies()
	if err != nil {
		return DailyReport{}, err
	}
	targetCount, err := runtime.activeRepositoryCount(ctx)
	if err != nil {
		return DailyReport{}, err
	}
	lastSuccess, err := runtime.lastProfileSuccess(ctx)
	if err != nil {
		return DailyReport{}, err
	}
	now := runtime.now().In(domain.ShanghaiLocation())
	discoveryReport := DiscoverReport{
		Source:      "all",
		DryRun:      true,
		Warnings:    []source.Warning{},
		Failures:    []OperationFailure{},
		Profiles:    []SearchProfileReport{},
		OSSEvidence: make(map[int64]snapshot.OSSEvidence),
	}
	for _, profile := range discoveryConfig.Profiles {
		if !profile.IsEnabled() {
			continue
		}
		var previous *time.Time
		if value, ok := lastSuccess[profile.Name]; ok {
			copyValue := value
			previous = &copyValue
		}
		due := profile.Due(now, previous)
		if profile.Schedule == "manual" {
			due = false
		}
		discoveryReport.Profiles = append(discoveryReport.Profiles, SearchProfileReport{Name: profile.Name, Due: due, Skipped: !due, QueryReports: []SearchQueryReport{}})
	}
	if discoveryConfig.Trending.IsEnabled() {
		complete, err := runtime.trendingCompletedToday(ctx, discoveryConfig.Trending.Periods)
		if err != nil {
			return DailyReport{}, err
		}
		discoveryReport.Trending = &TrendingDiscoveryReport{Periods: append([]string(nil), discoveryConfig.Trending.Periods...), Skipped: complete}
		if complete {
			discoveryReport.Trending.SkipReason = "already captured and resolved today"
		}
	}
	return DailyReport{
		DryRun:    true,
		Runtime:   runtimeReport,
		Discovery: discoveryReport,
		Snapshot: SnapshotCommandReport{Report: snapshot.Report{
			Date:        domain.ShanghaiDate(runtime.now()),
			TargetCount: targetCount,
			Failures:    []snapshot.Failure{},
		}, DryRun: true},
		Exports:  []exporter.Result{},
		Cleaned:  []string{},
		Failures: []OperationFailure{},
	}, nil
}

func dailyDetails(report DailyReport, client *github.Client) DailyJobDetails {
	details := DailyJobDetails{
		Runtime:   report.Runtime,
		Discovery: report.Discovery,
		Snapshot:  report.Snapshot.Report,
		Exports:   make([]string, 0, len(report.Exports)),
		Failures:  report.Failures,
		Cleaned:   report.Cleaned,
	}
	for _, exported := range report.Exports {
		details.Exports = append(details.Exports, exported.Path)
	}
	for _, failure := range report.Snapshot.Failures {
		details.FailureRepositories = append(details.FailureRepositories, failure.FullName)
	}
	for _, profile := range report.Discovery.Profiles {
		details.IncompleteResults = details.IncompleteResults || profile.IncompleteResults
		details.QuerySplitCount += profile.SplitCount
	}
	if client != nil {
		rates := client.RateLimits()
		if value, ok := rates[github.ResourceSearch]; ok {
			remaining := value.Remaining
			details.SearchRateRemaining = &remaining
		}
		if value, ok := rates[github.ResourceCore]; ok {
			remaining := value.Remaining
			details.CoreRateRemaining = &remaining
		}
	}
	return details
}

func summarizeDailyErrors(report DailyReport) string {
	count := len(report.Failures) + report.Discovery.FailureCount + report.Snapshot.FailureCount + report.Snapshot.MetadataFailureCount
	if count == 0 {
		return ""
	}
	return fmt.Sprintf("%d daily operations failed", count)
}

func cleanupRetention(directory string, days int, now time.Time) ([]string, error) {
	if strings.TrimSpace(directory) == "" || days <= 0 {
		return nil, fmt.Errorf("retention requires a directory and positive day count")
	}
	directory = filepath.Clean(directory)
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, fmt.Errorf("resolve retention directory: %w", err)
	}
	home, homeErr := os.UserHomeDir()
	resolved := absolute
	if evaluated, evaluateErr := filepath.EvalSymlinks(absolute); evaluateErr == nil {
		resolved = evaluated
	}
	if directory == "." || filepath.Dir(resolved) == resolved || (homeErr == nil && samePath(resolved, home)) {
		return nil, fmt.Errorf("refusing broad retention directory %s", directory)
	}
	entries, err := os.ReadDir(directory)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read retention directory: %w", err)
	}
	cutoff := now.UTC().AddDate(0, 0, -days)
	cleaned := make([]string, 0)
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), "github-radar-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return cleaned, fmt.Errorf("inspect retention entry %s: %w", entry.Name(), infoErr)
		}
		if !info.ModTime().UTC().Before(cutoff) {
			continue
		}
		path := filepath.Join(directory, entry.Name())
		if filepath.Dir(path) != directory {
			return cleaned, fmt.Errorf("refusing to remove retention path outside %s", directory)
		}
		if entry.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return cleaned, fmt.Errorf("remove expired export %s: %w", entry.Name(), err)
			}
		} else if err := os.Remove(path); err != nil {
			return cleaned, fmt.Errorf("remove expired export %s: %w", entry.Name(), err)
		}
		cleaned = append(cleaned, path)
	}
	return cleaned, nil
}

func samePath(left, right string) bool {
	leftPath, leftErr := filepath.Abs(left)
	rightPath, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftPath == rightPath
}

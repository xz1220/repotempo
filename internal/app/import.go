package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/jobs"
	"github.com/xz1220/repotempo/internal/source"
	csvsource "github.com/xz1220/repotempo/internal/source/csv"
	"github.com/xz1220/repotempo/internal/source/github"
	"github.com/xz1220/repotempo/internal/source/legacydb"
)

func (runtime *Runtime) ImportLegacy(ctx context.Context, options ImportOptions) (ImportReport, error) {
	report := ImportReport{
		DryRun:             options.DryRun,
		ObservationSources: make(map[string]int),
		Warnings:           []source.Warning{},
		Failures:           []OperationFailure{},
	}
	discoveryConfig, githubClient, _, err := runtime.dependencies()
	if err != nil {
		return report, err
	}
	legacyPath := firstNonEmpty(options.LegacyPath, runtime.settings.LegacyDatabasePath, discoveryConfig.Legacy.DatabasePath)
	if legacyPath == "" && len(options.CSVPaths) == 0 {
		return report, fmt.Errorf("import-legacy requires --path, --csv, or GITHUB_RADAR_LEGACY_DB_PATH")
	}
	if !options.DryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return report, err
		}
	}

	var execution *jobs.Execution
	if !options.DryRun {
		execution, err = runtime.tracker().Start(ctx, "import-legacy", map[string]any{
			"legacy_configured": legacyPath != "",
			"csv_count":         len(options.CSVPaths),
		})
		if err != nil {
			return report, err
		}
	}

	candidates := make([]source.Candidate, 0)
	observations := make([]source.Observation, 0)
	if legacyPath != "" {
		legacyResult, legacyErr := runtime.loadLegacy(ctx, legacyPath)
		if legacyErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "legacy", Target: legacyPath, Message: legacyErr.Error()})
		} else {
			stats := legacyResult.Stats
			report.Legacy = &stats
			report.Warnings = append(report.Warnings, legacyResult.Warnings...)
			candidates = append(candidates, legacyResult.Candidates...)
			observations = append(observations, legacyResult.Observations...)
		}
	}

	for _, path := range options.CSVPaths {
		file, openErr := os.Open(path)
		if openErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "csv", Target: path, Message: openErr.Error()})
			continue
		}
		csvResult, importErr := csvsource.Import(file, csvsource.Options{Location: domain.ShanghaiLocation(), DefaultSource: "legacy"})
		closeErr := file.Close()
		if importErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "csv", Target: path, Message: importErr.Error()})
			continue
		}
		if closeErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "csv", Target: path, Message: closeErr.Error()})
		}
		report.Warnings = append(report.Warnings, csvResult.Warnings...)
		verified, allowedIDs := verifyCSVCandidates(ctx, githubClient, csvResult.Candidates, &report)
		candidates = append(candidates, verified...)
		for _, observation := range csvResult.Observations {
			if _, allowed := allowedIDs[observation.RepositoryID]; allowed {
				observations = append(observations, observation)
			} else {
				report.Failures = append(report.Failures, OperationFailure{
					Stage: "csv-observation", Target: fmt.Sprintf("%d/%s", observation.RepositoryID, observation.SnapshotDate.Format("2006-01-02")),
					Message: "observation rejected because the repository identity was not verified by GitHub",
				})
			}
		}
	}

	merged := source.MergeCandidates(candidates)
	report.CandidateCount = len(merged)
	report.ObservationCount = len(observations)
	for _, observation := range observations {
		report.ObservationSources[observation.Source]++
		if !observation.FixedPanel {
			report.NonFixedObservationCount++
		}
	}
	if options.DryRun {
		report.FailureCount = len(report.Failures)
		return runtime.finishImport(ctx, execution, report, nil)
	}

	registryReport, mergeErr := runtime.registry().Merge(ctx, merged)
	if mergeErr != nil {
		return runtime.finishImport(ctx, execution, report, mergeErr)
	}
	report.Registry = &registryReport
	for _, failure := range registryReport.Failures {
		report.Failures = append(report.Failures, OperationFailure{Stage: "registry", Target: failure.FullName, Message: failure.Error})
	}
	runtime.preservePreviousNames(ctx, merged, registryReport.Repositories, &report.Failures)
	topicReport := DiscoverReport{Warnings: []source.Warning{}, Failures: []OperationFailure{}}
	_ = runtime.assignCandidateTopics(ctx, candidates, registryReport.Repositories, &topicReport)
	report.Warnings = append(report.Warnings, topicReport.Warnings...)
	report.Failures = append(report.Failures, topicReport.Failures...)

	storedIDs := make(map[int64]struct{}, len(registryReport.Repositories))
	for _, repository := range registryReport.Repositories {
		storedIDs[repository.GitHubRepoID] = struct{}{}
	}
	for _, observation := range observations {
		if _, stored := storedIDs[observation.RepositoryID]; !stored {
			report.Failures = append(report.Failures, OperationFailure{
				Stage: "observation", Target: fmt.Sprintf("%d/%s", observation.RepositoryID, observation.SnapshotDate.Format("2006-01-02")),
				Message: "repository was not imported",
			})
			continue
		}
		stars := observation.StarCount
		snapshotValue := domain.DailySnapshot{
			RepositoryID: observation.RepositoryID,
			SnapshotDate: domain.ShanghaiDate(observation.SnapshotDate),
			CapturedAt:   observation.ObservedAt.UTC(),
			StarCount:    &stars,
			FetchStatus:  domain.FetchSuccess,
		}
		if evidenceErr := applyImportedEvidence(&snapshotValue, observation.Metadata); evidenceErr != nil {
			report.Failures = append(report.Failures, OperationFailure{
				Stage: "observation-evidence", Target: fmt.Sprintf("%d/%s", observation.RepositoryID, observation.SnapshotDate.Format("2006-01-02")), Message: evidenceErr.Error(),
			})
		}
		write, writeErr := runtime.store.PutDailySnapshot(ctx, snapshotValue)
		if writeErr != nil {
			report.Failures = append(report.Failures, OperationFailure{
				Stage: "observation", Target: fmt.Sprintf("%d/%s", observation.RepositoryID, observation.SnapshotDate.Format("2006-01-02")), Message: writeErr.Error(),
			})
			continue
		}
		switch write.Disposition {
		case domain.SnapshotInserted:
			report.InsertedCount++
		case domain.SnapshotFailureRepaired:
			report.RepairedCount++
		case domain.SnapshotSuccessProtected, domain.SnapshotUnchanged:
			report.ProtectedCount++
		}
	}
	report.FailureCount = len(report.Failures)
	return runtime.finishImport(ctx, execution, report, nil)
}

func applyImportedEvidence(snapshotValue *domain.DailySnapshot, metadata map[string]string) error {
	parseInteger := func(keys ...string) (*int64, error) {
		for _, key := range keys {
			raw := metadata[key]
			if raw == "" {
				continue
			}
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s must be an integer", key)
			}
			return &value, nil
		}
		return nil, nil
	}
	rank, err := parseInteger("oss_today_rank", "legacy_rank")
	if err != nil {
		return err
	}
	if rank != nil {
		if *rank <= 0 || *rank > int64(^uint(0)>>1) {
			return fmt.Errorf("OSS rank is outside the supported range")
		}
		value := int(*rank)
		snapshotValue.OSSTodayRank = &value
	}
	windowStars, err := parseInteger("oss_window_stars", "legacy_window_stars")
	if err != nil {
		return err
	}
	if windowStars != nil {
		snapshotValue.OSSWindowStars = windowStars
	}
	if raw := metadata["oss_total_score"]; raw != "" {
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return fmt.Errorf("oss_total_score must be numeric")
		}
		snapshotValue.OSSTotalScore = &value
	}
	return nil
}

func verifyCSVCandidates(ctx context.Context, client *github.Client, candidates []source.Candidate, report *ImportReport) ([]source.Candidate, map[int64]struct{}) {
	verified := make([]source.Candidate, 0, len(candidates))
	allowed := make(map[int64]struct{}, len(candidates))
	for _, candidate := range candidates {
		observedFullName := candidate.Repository.FullName
		repository, err := client.ResolveRepository(ctx, candidate.Repository.FullName, candidate.Repository.ID)
		if err != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "csv-identity", Target: candidate.Repository.FullName, Message: err.Error()})
			continue
		}
		candidate.Repository.NodeID = repository.NodeID
		candidate.Repository.FullName = repository.FullName
		if observedFullName != "" && observedFullName != repository.FullName {
			candidate.PreviousNames = appendUniqueStrings(candidate.PreviousNames, observedFullName)
		}
		candidate.Repository.HTMLURL = repository.HTMLURL
		candidate.Repository.Description = repository.Description
		candidate.Repository.Language = repository.Language
		candidate.Repository.AbsoluteStars = repository.AbsoluteStars
		candidate.Repository.Forks = repository.Forks
		candidate.Repository.Fork = repository.Fork
		candidate.Repository.Archived = repository.Archived
		candidate.Repository.Private = repository.Private
		candidate.Repository.CreatedAt = repository.CreatedAt
		candidate.Repository.UpdatedAt = repository.UpdatedAt
		candidate.Repository.PushedAt = repository.PushedAt
		candidate.Repository.Topics = appendUniqueStrings(candidate.Repository.Topics, repository.Topics...)
		verified = append(verified, candidate)
		allowed[candidate.Repository.ID] = struct{}{}
	}
	return verified, allowed
}

func appendUniqueStrings(values []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(values)+len(additions))
	for _, value := range values {
		seen[value] = struct{}{}
	}
	for _, value := range additions {
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

func (runtime *Runtime) finishImport(ctx context.Context, execution *jobs.Execution, report ImportReport, runErr error) (ImportReport, error) {
	if execution == nil {
		return report, runErr
	}
	status := domain.JobSuccess
	errorSummary := ""
	if runErr != nil {
		status = domain.JobFailed
		errorSummary = runErr.Error()
	} else if report.Partial() {
		status = domain.JobPartial
		errorSummary = summarizeFailures(report.Failures)
	}
	finishErr := execution.Finish(context.WithoutCancel(ctx), jobs.Outcome{
		Status:       status,
		TargetCount:  report.ObservationCount,
		SuccessCount: report.InsertedCount + report.RepairedCount,
		FailureCount: report.FailureCount,
		SkippedCount: report.ProtectedCount,
		Details:      report,
		ErrorSummary: errorSummary,
	})
	if runErr != nil {
		return report, errors.Join(runErr, finishErr)
	}
	return report, finishErr
}

func (runtime *Runtime) loadLegacy(ctx context.Context, path string) (legacydb.Result, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return legacydb.Result{}, fmt.Errorf("resolve legacy database path: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return legacydb.Result{}, fmt.Errorf("read legacy database: %w", err)
	}
	if info.IsDir() {
		return legacydb.Result{}, fmt.Errorf("legacy database path is a directory")
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute, RawQuery: "mode=ro"}).String()
	database, err := sql.Open("sqlite", dsn)
	if err != nil {
		return legacydb.Result{}, fmt.Errorf("open legacy database: %w", err)
	}
	defer database.Close()
	if err := database.PingContext(ctx); err != nil {
		return legacydb.Result{}, fmt.Errorf("ping legacy database: %w", err)
	}
	result, err := legacydb.Import(ctx, database, legacydb.Options{Location: domain.ShanghaiLocation()})
	if err != nil {
		return result, fmt.Errorf("import legacy database: %w", err)
	}
	return result, nil
}

func observationDate(value time.Time) string {
	return value.In(domain.ShanghaiLocation()).Format("2006-01-02")
}

package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/config"
	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/service/jobs"
	"github.com/xz1220/github-radar/internal/service/snapshot"
	"github.com/xz1220/github-radar/internal/source"
	"github.com/xz1220/github-radar/internal/source/github"
	"github.com/xz1220/github-radar/internal/source/manual"
	"github.com/xz1220/github-radar/internal/source/ossinsight"
	corestore "github.com/xz1220/github-radar/internal/store"
)

func (runtime *Runtime) Discover(ctx context.Context, options DiscoverOptions) (DiscoverReport, error) {
	if !validDiscoverSource(options.Source) {
		return DiscoverReport{}, fmt.Errorf("discover source must be ossinsight, github-search, legacy, or all")
	}
	if options.Profile != "" && options.Source != "github-search" && options.Source != "all" {
		return DiscoverReport{}, fmt.Errorf("--profile requires source github-search or all")
	}
	if options.Source == "all" && !options.IncludeLegacy && !options.IncludeManual {
		options.IncludeLegacy = true
		options.IncludeManual = true
	}
	if !options.DryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return DiscoverReport{}, err
		}
	}

	var execution *jobs.Execution
	var err error
	if !options.DryRun {
		execution, err = runtime.tracker().Start(ctx, "discover", map[string]any{
			"source": options.Source, "profile": options.Profile,
		})
		if err != nil {
			return DiscoverReport{}, err
		}
	}

	report, runErr := runtime.runDiscovery(ctx, options)
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
		TargetCount:  report.CandidateCount,
		SuccessCount: report.CreatedCount + report.UpdatedCount,
		FailureCount: report.FailureCount,
		SkippedCount: report.SkippedCount,
		Details:      report,
		ErrorSummary: errorSummary,
	})
	if runErr != nil {
		if finishErr != nil {
			return report, errors.Join(runErr, finishErr)
		}
		return report, runErr
	}
	return report, finishErr
}

func (runtime *Runtime) runDiscovery(ctx context.Context, options DiscoverOptions) (DiscoverReport, error) {
	report := DiscoverReport{
		Source:      options.Source,
		DryRun:      options.DryRun,
		Warnings:    []source.Warning{},
		Failures:    []OperationFailure{},
		Profiles:    []SearchProfileReport{},
		OSSEvidence: make(map[int64]snapshot.OSSEvidence),
	}
	discoveryConfig, githubClient, ossClient, err := runtime.dependencies()
	if err != nil {
		return report, err
	}
	now := runtime.now().UTC()
	searchNow := now.In(domain.ShanghaiLocation())
	candidates := make([]source.Candidate, 0)

	if options.Source == "ossinsight" || options.Source == "all" {
		if discoveryConfig.OSSInsight.Enabled != nil && !*discoveryConfig.OSSInsight.Enabled {
			report.Warnings = append(report.Warnings, source.Warning{Code: "ossinsight_disabled", Message: "OSS Insight is disabled by configuration"})
		} else {
			ossResult, ossErr := ossClient.FetchWindows(ctx, discoveryConfig.OSSInsight.Windows, discoveryConfig.OSSInsight.Language)
			report.OSS = mapOSSReport(ossResult, ossErr)
			report.Warnings = append(report.Warnings, ossResult.Warnings...)
			candidates = append(candidates, ossResult.Candidates(now)...)
			report.OSSEvidence = ossEvidence(ossResult, discoveryConfig.OSSInsight.Windows)
			if ossErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "ossinsight", Message: ossErr.Error()})
			}
		}
	}

	if options.Source == "github-search" || options.Source == "all" {
		profiles, selectErr := selectProfiles(discoveryConfig.Profiles, options.Profile)
		if selectErr != nil {
			return report, selectErr
		}
		lastSuccess := map[string]time.Time{}
		if options.DueOnly && options.Profile == "" {
			lastSuccess, err = runtime.lastProfileSuccess(ctx)
			if err != nil {
				return report, err
			}
		}
		for _, profile := range profiles {
			var last *time.Time
			if value, ok := lastSuccess[profile.Name]; ok {
				copyValue := value
				last = &copyValue
			}
			due := !options.DueOnly || options.Profile != "" || profile.Due(searchNow, last)
			if options.DueOnly && options.Profile == "" && profile.Schedule == "manual" {
				due = false
			}
			profileReport := SearchProfileReport{Name: profile.Name, Due: due, Skipped: !due}
			if !due {
				report.Profiles = append(report.Profiles, profileReport)
				continue
			}
			result, searchErr := githubClient.SearchProfile(ctx, profile, searchNow)
			profileReport = mapSearchProfileReport(result, searchErr)
			profileReport.Due = true
			report.Profiles = append(report.Profiles, profileReport)
			candidates = append(candidates, result.Candidates(now)...)
			if searchErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "github-search", Target: profile.Name, Message: searchErr.Error()})
			}
		}
	}

	if options.Source == "legacy" || (options.Source == "all" && options.IncludeLegacy) {
		legacyPath := firstNonEmpty(options.LegacyPath, runtime.settings.LegacyDatabasePath, discoveryConfig.Legacy.DatabasePath)
		if legacyPath == "" {
			if options.Source == "legacy" {
				return report, fmt.Errorf("legacy database path is required; set --path or GITHUB_RADAR_LEGACY_DB_PATH")
			}
			report.Warnings = append(report.Warnings, source.Warning{Code: "legacy_not_configured", Message: "legacy discovery was skipped because no database path is configured"})
		} else {
			legacyResult, legacyErr := runtime.loadLegacy(ctx, legacyPath)
			if legacyErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "legacy", Target: legacyPath, Message: legacyErr.Error()})
			} else {
				stats := legacyResult.Stats
				report.Legacy = &stats
				report.Warnings = append(report.Warnings, legacyResult.Warnings...)
				candidates = append(candidates, legacyResult.Candidates...)
			}
		}
	}

	if options.Source == "all" && options.IncludeManual {
		for _, path := range discoveryConfig.Manual.Files {
			file, openErr := os.Open(path)
			if openErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "manual", Target: path, Message: openErr.Error()})
				continue
			}
			manualResult, importErr := manual.Import(ctx, file, githubClient, now)
			closeErr := file.Close()
			if importErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "manual", Target: path, Message: importErr.Error()})
				continue
			}
			if closeErr != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "manual", Target: path, Message: closeErr.Error()})
			}
			report.Warnings = append(report.Warnings, manualResult.Warnings...)
			candidates = append(candidates, manualResult.Candidates...)
		}
	}

	merged := source.MergeCandidates(candidates)
	report.CandidateCount = len(merged)
	if options.DryRun {
		report.FailureCount = len(report.Failures)
		return report, nil
	}
	registryReport, err := runtime.registry().Merge(ctx, merged)
	if err != nil {
		return report, err
	}
	report.Registry = &registryReport
	report.CreatedCount = registryReport.CreatedCount
	report.UpdatedCount = registryReport.UpdatedCount
	report.SkippedCount = registryReport.SkippedCount
	for _, failure := range registryReport.Failures {
		report.Failures = append(report.Failures, OperationFailure{Stage: "registry", Target: failure.FullName, Message: failure.Error})
	}
	runtime.preservePreviousNames(ctx, merged, registryReport.Repositories, &report.Failures)
	report.TopicAssignments = runtime.assignCandidateTopics(ctx, candidates, registryReport.Repositories, &report)
	report.FailureCount = len(report.Failures)
	return report, nil
}

func validDiscoverSource(value string) bool {
	switch value {
	case "ossinsight", "github-search", "legacy", "all":
		return true
	default:
		return false
	}
}

func selectProfiles(profiles []config.SearchProfile, name string) ([]config.SearchProfile, error) {
	selected := make([]config.SearchProfile, 0, len(profiles))
	for _, profile := range profiles {
		if name != "" && profile.Name != name {
			continue
		}
		if name == "" && !profile.IsEnabled() {
			continue
		}
		selected = append(selected, profile)
	}
	if name != "" && len(selected) == 0 {
		return nil, fmt.Errorf("GitHub Search profile %q was not found", name)
	}
	return selected, nil
}

func mapSearchProfileReport(result github.SearchResult, err error) SearchProfileReport {
	report := SearchProfileReport{
		Name:              result.Profile,
		HitCount:          len(result.Hits),
		TotalCount:        result.TotalCount,
		QueryCount:        len(result.Reports),
		IncompleteResults: result.IncompleteResults,
		Truncated:         result.Truncated,
		QueryReports:      mapQueryReports(result.Reports),
	}
	for _, query := range result.Reports {
		report.PageCount += query.Pages
		if query.Split {
			report.SplitCount++
		}
	}
	if result.RateLimit.Resource != "" {
		remaining := result.RateLimit.Remaining
		report.RateRemaining = &remaining
		report.RateReset = result.RateLimit.Reset
	}
	if err != nil {
		report.Error = err.Error()
	}
	return report
}

func mapQueryReports(values []github.QueryReport) []SearchQueryReport {
	result := make([]SearchQueryReport, 0, len(values))
	for _, value := range values {
		mapped := SearchQueryReport{
			Query:             value.Query,
			Depth:             value.Depth,
			TotalCount:        value.TotalCount,
			IncompleteResults: value.IncompleteResults,
			Pages:             value.Pages,
			Split:             value.Split,
			Truncated:         value.Truncated,
		}
		if value.RateLimit.Resource != "" {
			remaining := value.RateLimit.Remaining
			mapped.RateRemaining = &remaining
			mapped.RateReset = value.RateLimit.Reset
		}
		result = append(result, mapped)
	}
	return result
}

func mapOSSReport(result ossinsight.Result, err error) *OSSDiscoveryReport {
	report := &OSSDiscoveryReport{
		RepositoryCount: len(result.Repositories),
		RowsByWindow:    result.RowsByWindow,
		WarningCount:    len(result.Warnings),
	}
	if err != nil {
		report.Error = err.Error()
	}
	return report
}

func ossEvidence(result ossinsight.Result, windows []config.OSSWindow) map[int64]snapshot.OSSEvidence {
	todayWindow := "today"
	for _, window := range windows {
		if window.Period == "past_24_hours" {
			todayWindow = window.Name
			break
		}
	}
	evidence := make(map[int64]snapshot.OSSEvidence)
	for _, repository := range result.Repositories {
		signal, ok := repository.Signals[todayWindow]
		if !ok {
			continue
		}
		rank := signal.Rank
		evidence[repository.Repository.ID] = snapshot.OSSEvidence{
			TodayRank:   &rank,
			WindowStars: signal.WindowStars,
			TotalScore:  signal.TotalScore,
		}
	}
	return evidence
}

func (runtime *Runtime) assignCandidateTopics(ctx context.Context, candidates []source.Candidate, repositories []domain.Repository, report *DiscoverReport) int {
	stored := make(map[int64]struct{}, len(repositories))
	for _, repository := range repositories {
		stored[repository.GitHubRepoID] = struct{}{}
	}
	seen := make(map[string]int)
	assigned := 0
	for _, candidate := range candidates {
		if _, ok := stored[candidate.Repository.ID]; !ok {
			continue
		}
		assignmentSource, confirmed := candidateTopicSource(candidate)
		priority := topicSourcePriority(assignmentSource)
		for _, slug := range candidate.Repository.Topics {
			slug = strings.TrimSpace(strings.ToLower(slug))
			key := fmt.Sprintf("%d/%s", candidate.Repository.ID, slug)
			if slug == "" {
				continue
			}
			if previousPriority, duplicate := seen[key]; duplicate && previousPriority >= priority {
				continue
			}
			seen[key] = priority
			topicValue, err := runtime.store.GetTopicBySlug(ctx, slug)
			if errors.Is(err, corestore.ErrNotFound) {
				if candidate.Source == "manual" {
					report.Warnings = append(report.Warnings, source.Warning{Code: "unknown_manual_topic", Message: slug})
				}
				continue
			}
			if err != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "topic", Target: key, Message: err.Error()})
				continue
			}
			confidence := 1.0
			result, err := runtime.store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
				RepositoryID: candidate.Repository.ID,
				TopicID:      topicValue.ID,
				Source:       assignmentSource,
				Confirmed:    confirmed,
				Confidence:   &confidence,
				AssignedAt:   runtime.now().UTC(),
			})
			if err != nil {
				report.Failures = append(report.Failures, OperationFailure{Stage: "topic", Target: key, Message: err.Error()})
				continue
			}
			if result.Changed {
				assigned++
			}
		}
	}
	return assigned
}

func topicSourcePriority(value domain.TopicSource) int {
	switch value {
	case domain.TopicSourceManual:
		return 4
	case domain.TopicSourceGitHub:
		return 3
	case domain.TopicSourceImported:
		return 2
	default:
		return 1
	}
}

func (runtime *Runtime) preservePreviousNames(ctx context.Context, candidates []source.Candidate, repositories []domain.Repository, failures *[]OperationFailure) {
	stored := make(map[int64]domain.Repository, len(repositories))
	for _, repository := range repositories {
		stored[repository.GitHubRepoID] = repository
	}
	seen := make(map[string]struct{})
	for _, candidate := range candidates {
		repository, ok := stored[candidate.Repository.ID]
		if !ok {
			continue
		}
		discoverySource, ok := candidateDiscoverySource(candidate.Source)
		if !ok {
			continue
		}
		observedAt := candidate.DiscoveredAt
		if observedAt.IsZero() || !observedAt.Before(repository.LastDiscoveredAt) {
			observedAt = repository.LastDiscoveredAt.Add(-time.Nanosecond)
		}
		for _, previousName := range candidate.PreviousNames {
			previousName = strings.TrimSpace(previousName)
			key := fmt.Sprintf("%d/%s", candidate.Repository.ID, strings.ToLower(previousName))
			if previousName == "" || strings.EqualFold(previousName, repository.FullName) {
				continue
			}
			if _, duplicate := seen[key]; duplicate {
				continue
			}
			seen[key] = struct{}{}
			if _, _, err := runtime.store.UpsertRepository(ctx, domain.RepositoryObservation{
				GitHubRepoID: candidate.Repository.ID,
				FullName:     previousName,
				Source:       discoverySource,
				Profile:      candidate.Profile,
				DiscoveredAt: observedAt,
			}); err != nil {
				*failures = append(*failures, OperationFailure{Stage: "previous-name", Target: previousName, Message: err.Error()})
			}
		}
	}
}

func candidateDiscoverySource(value string) (domain.DiscoverySource, bool) {
	switch value {
	case "ossinsight":
		return domain.DiscoverySourceOSSInsight, true
	case "github_search", "github-search":
		return domain.DiscoverySourceGitHubSearch, true
	case "legacy":
		return domain.DiscoverySourceLegacy, true
	case "manual":
		return domain.DiscoverySourceManual, true
	default:
		return "", false
	}
}

func candidateTopicSource(candidate source.Candidate) (domain.TopicSource, bool) {
	if candidate.Profile == "config-watchlist" {
		return domain.TopicSourceAuto, false
	}
	switch candidate.Source {
	case "manual":
		return domain.TopicSourceManual, true
	case "legacy":
		return domain.TopicSourceImported, false
	case "github_search", "github-search":
		return domain.TopicSourceGitHub, false
	default:
		return domain.TopicSourceAuto, false
	}
}

func (runtime *Runtime) lastProfileSuccess(ctx context.Context) (map[string]time.Time, error) {
	if runtime.planningDB != nil {
		return runtime.planningProfileSuccess(ctx)
	}
	runs, err := runtime.store.ListJobRuns(ctx, 0, 0)
	if err != nil {
		return nil, fmt.Errorf("read prior discovery runs: %w", err)
	}
	result := make(map[string]time.Time)
	for _, run := range runs {
		var details struct {
			Profiles  []SearchProfileReport `json:"profiles"`
			Discovery *struct {
				Profiles []SearchProfileReport `json:"profiles"`
			} `json:"discovery"`
		}
		if json.Unmarshal(run.Details, &details) != nil {
			continue
		}
		profiles := details.Profiles
		if details.Discovery != nil {
			profiles = append(profiles, details.Discovery.Profiles...)
		}
		when := run.StartedAt
		if run.FinishedAt != nil {
			when = *run.FinishedAt
		}
		for _, profile := range profiles {
			if profile.Skipped || profile.Error != "" || profile.IncompleteResults || profile.Truncated {
				continue
			}
			if previous, ok := result[profile.Name]; !ok || when.After(previous) {
				result[profile.Name] = when
			}
		}
	}
	return result, nil
}

func summarizeFailures(failures []OperationFailure) string {
	if len(failures) == 0 {
		return ""
	}
	if len(failures) == 1 {
		return failures[0].Message
	}
	return fmt.Sprintf("%d operations failed", len(failures))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

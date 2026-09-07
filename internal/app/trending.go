package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source"
	"github.com/xz1220/repotempo/internal/source/github"
)

func (runtime *Runtime) discoverTrending(ctx context.Context, options DiscoverOptions, cfg config.Discovery, client *github.Client, report *DiscoverReport) []source.Candidate {
	if !cfg.Trending.IsEnabled() {
		if options.Source == "github-trending" {
			report.Failures = append(report.Failures, OperationFailure{Stage: "github-trending", Message: "GitHub Trending is disabled by configuration"})
		}
		return nil
	}
	part := &TrendingDiscoveryReport{Periods: append([]string(nil), cfg.Trending.Periods...), ResolvedIDs: map[string]int64{}}
	report.Trending = part
	if options.DueOnly {
		complete, err := runtime.trendingCompletedToday(ctx, cfg.Trending.Periods)
		if err != nil {
			part.FailureCount++
			report.Failures = append(report.Failures, OperationFailure{Stage: "github-trending-plan", Message: err.Error()})
			return nil
		}
		if complete {
			part.Skipped, part.SkipReason = true, "already captured and resolved today"
			return nil
		}
	}
	result, fetchErr := runtime.trending.FetchWindows(ctx, cfg.Trending.Periods)
	part.Windows = result.Windows
	for _, window := range result.Windows {
		if window.Error != "" {
			part.FailureCount++
			report.Failures = append(report.Failures, OperationFailure{Stage: "github-trending", Target: window.Period, Message: window.Error})
		}
	}
	if fetchErr != nil && part.FailureCount == 0 {
		part.FailureCount++
		report.Failures = append(report.Failures, OperationFailure{Stage: "github-trending", Message: fetchErr.Error()})
	}
	var candidates []source.Candidate
	seen := map[string]bool{}
	for _, window := range result.Windows {
		if window.Error != "" {
			continue // A partial parse remains evidence, not a complete ranking.
		}
		for _, entry := range window.Entries {
			key := strings.ToLower(entry.FullName)
			if seen[key] {
				continue
			}
			seen[key] = true
			repository, err := client.ResolveRepository(ctx, entry.FullName, 0)
			if err != nil || repository.Private || repository.ID <= 0 {
				message := "repository is private or has no valid GitHub identity"
				if err != nil {
					message = err.Error()
				}
				part.FailureCount++
				report.Failures = append(report.Failures, OperationFailure{Stage: "github-trending-resolve", Target: entry.FullName, Message: message})
				var apiError *github.APIError
				if ctx.Err() != nil || (errors.As(err, &apiError) && (apiError.StatusCode == 401 || apiError.StatusCode == 403 || apiError.StatusCode == 429)) {
					return candidates
				}
				continue
			}
			part.ResolvedCount++
			part.ResolvedIDs[entry.FullName] = repository.ID
			candidate := source.Candidate{
				Repository: repository, Source: string(domain.DiscoverySourceGitHubTrending),
				Profile: "trending-" + window.Period, DiscoveredAt: window.CapturedAt,
			}
			if !strings.EqualFold(entry.FullName, repository.FullName) {
				candidate.PreviousNames = []string{entry.FullName}
			}
			candidates = append(candidates, candidate)
		}
	}
	return candidates
}

// Successful discovery jobs are the durable once-a-day marker. Failed/partial
// parses or identity resolution failures are retried, without inventing evidence.
func (runtime *Runtime) trendingCompletedToday(ctx context.Context, periods []string) (bool, error) {
	today := domain.ShanghaiDate(runtime.now())
	complete := func(raw []byte) bool {
		var details struct {
			Trending *TrendingDiscoveryReport `json:"trending"`
		}
		if json.Unmarshal(raw, &details) != nil || details.Trending == nil {
			return false
		}
		part := details.Trending
		if part.Skipped || part.FailureCount != 0 || part.ResolvedCount == 0 {
			return false
		}
		captured := map[string]bool{}
		for _, window := range part.Windows {
			if window.Error == "" && len(window.Entries) > 0 && domain.ShanghaiDate(window.CapturedAt) == today {
				captured[window.Period] = true
			}
		}
		for _, period := range periods {
			if !captured[period] {
				return false
			}
		}
		return len(periods) > 0
	}
	if runtime.planningDB != nil {
		rows, err := runtime.planningDB.QueryContext(ctx, `SELECT details_json FROM job_runs WHERE job_type='discover' AND status='success' AND finished_at IS NOT NULL AND date(started_at,'+8 hours')=? ORDER BY started_at DESC`, today)
		if err != nil {
			return false, fmt.Errorf("read Trending schedule: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if err := rows.Scan(&raw); err != nil {
				return false, err
			}
			if complete(raw) {
				return true, nil
			}
		}
		return false, rows.Err()
	}
	for offset := 0; ; offset += 20 {
		runs, err := runtime.store.ListJobRuns(ctx, 20, offset)
		if err != nil {
			return false, err
		}
		for _, run := range runs {
			if domain.ShanghaiDate(run.StartedAt) < today {
				return false, nil
			}
			if run.JobType == "discover" && run.Status == domain.JobSuccess && run.FinishedAt != nil && complete(run.Details) {
				return true, nil
			}
		}
		if len(runs) < 20 {
			return false, nil
		}
	}
}

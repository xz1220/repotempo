package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
)

type ActivityOptions struct {
	Repository string
	Date       string
	Limit      int
	MaxPages   int
	Force      bool
}

type ActivityReport struct {
	TargetCount    int                `json:"target_count"`
	SuccessCount   int                `json:"success_count"`
	TruncatedCount int                `json:"truncated_count"`
	SkippedCount   int                `json:"skipped_count"`
	DeferredReason string             `json:"deferred_reason,omitempty"`
	Failures       []OperationFailure `json:"failures"`
}

func validateActivityOptions(options ActivityOptions) error {
	if options.Limit < 1 || options.Limit > 500 || options.MaxPages < 1 || options.MaxPages > 5 {
		return errors.New("activity requires --limit between 1 and 500 and --max-pages between 1 and 5")
	}
	if options.Repository != "" && options.Date != "" {
		return errors.New("--repository and --date cannot be combined")
	}
	if options.Date != "" {
		date, err := time.Parse("2006-01-02", options.Date)
		if err != nil || date.Format("2006-01-02") != options.Date {
			return errors.New("--date must be YYYY-MM-DD (the first-collected date, not a historical activity snapshot)")
		}
	}
	return nil
}

func (runtime *Runtime) RefreshActivity(ctx context.Context, options ActivityOptions) (ActivityReport, error) {
	report := ActivityReport{Failures: []OperationFailure{}}
	if err := validateActivityOptions(options); err != nil {
		return report, err
	}
	now := runtime.now().UTC()
	staleBefore := now.AddDate(0, 0, -7)
	if options.Force {
		// SQLite julianday comparisons do not preserve nanosecond precision.
		staleBefore = now.Add(time.Second)
	}
	var repositories []domain.Repository
	if options.Repository != "" {
		repository, err := runtime.store.GetRepositoryByFullName(ctx, strings.TrimSpace(options.Repository))
		if err != nil {
			return report, err
		}
		if repository.MonitoringStatus != domain.MonitoringActive || (repository.GitHubStatus != domain.GitHubActive && repository.GitHubStatus != domain.GitHubArchived) {
			return report, errors.New("activity collection requires an active, public monitored repository")
		}
		if !options.Force && repository.Activity != nil && !repository.Activity.FetchedAt.Before(staleBefore) {
			report.SkippedCount = 1
			return report, nil
		}
		repositories = []domain.Repository{repository}
	} else {
		var since, until *time.Time
		if options.Date != "" {
			start, _ := time.ParseInLocation("2006-01-02", options.Date, domain.ShanghaiLocation())
			end := start.AddDate(0, 0, 1)
			since, until = &start, &end
		}
		var err error
		repositories, err = runtime.store.ActivityCandidates(ctx, since, until, staleBefore, options.Limit)
		if err != nil {
			return report, err
		}
	}
	report.TargetCount = len(repositories)
	if len(repositories) == 0 {
		return report, nil
	}
	_, client, _, err := runtime.dependencies()
	if err != nil {
		return report, err
	}
	for index, repository := range repositories {
		if err := ctx.Err(); err != nil {
			report.DeferredReason = "time limit reached; unfinished repositories retain their previous activity data"
			report.SkippedCount += len(repositories) - index
			return report, err
		}
		// Activity is supplementary. Leave API headroom for Star observations,
		// and never wait for the next hourly quota merely to fill this cache.
		if rate, ok := client.RateLimits()[github.ResourceCore]; ok && rate.Limit > 0 && rate.Remaining < 100 && rate.Reset.After(runtime.now()) {
			report.DeferredReason = "GitHub API headroom reserved for Star collection"
			report.SkippedCount += len(repositories) - index
			break
		}
		activity, fetchErr := client.FetchActivity(ctx, repository.GitHubRepoID, runtime.now(), options.MaxPages)
		if fetchErr != nil {
			report.Failures = append(report.Failures, OperationFailure{Stage: "activity", Target: repository.FullName, Message: fetchErr.Error()})
			if github.IsAccessBlocked(fetchErr) || ctx.Err() != nil {
				report.DeferredReason = "GitHub activity collection interrupted; previous data preserved"
				report.SkippedCount += len(repositories) - index - 1
				break
			}
			continue
		}
		if err := runtime.store.PutRepositoryActivity(ctx, repository.GitHubRepoID, activity); err != nil {
			return report, fmt.Errorf("save activity for %s: %w", repository.FullName, err)
		}
		report.SuccessCount++
		if !activity.Complete {
			report.TruncatedCount++
		}
	}
	return report, nil
}

func (cli *CLI) runActivity(ctx context.Context, settings Settings, jsonOutput bool, args []string) int {
	const command = "activity refresh"
	if len(args) == 0 || args[0] != "refresh" {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, errors.New("activity requires refresh"))
	}
	flags := cli.flagSet(command)
	repository := flags.String("repository", "", "one registered owner/repository")
	date := flags.String("date", "", "limit to repositories first collected on this Shanghai date (YYYY-MM-DD)")
	limit := flags.Int("limit", 100, "maximum repositories, 1 to 500")
	pages := flags.Int("max-pages", 3, "maximum 100-commit pages per repository, 1 to 5")
	force := flags.Bool("force", false, "refresh even when the seven-day cache is fresh")
	if err := parseFlags(flags, args[1:]); err != nil {
		return cli.flagError(command, jsonOutput, settings, err)
	}
	options := ActivityOptions{Repository: strings.TrimSpace(*repository), Date: *date, Limit: *limit, MaxPages: *pages, Force: *force}
	if err := validateActivityOptions(options); err != nil {
		return cli.writeError(command, jsonOutput, settings, ExitUsage, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	return cli.invoke(ctx, settings, jsonOutput, command, func(application CommandApplication) (any, string, int, error) {
		collector, ok := application.(interface {
			RefreshActivity(context.Context, ActivityOptions) (ActivityReport, error)
		})
		if !ok {
			return nil, "", ExitFailure, errors.New("activity refresh is not supported by this runtime")
		}
		report, err := collector.RefreshActivity(ctx, options)
		code := ExitSuccess
		if len(report.Failures) > 0 || report.DeferredReason != "" {
			code = ExitPartial
		}
		return report, fmt.Sprintf("Updated %d activity records; %d bounded samples; %d deferred. %s", report.SuccessCount, report.TruncatedCount, report.SkippedCount, report.DeferredReason), code, err
	})
}

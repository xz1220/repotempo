package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/xz1220/github-radar/internal/domain"
	"github.com/xz1220/github-radar/internal/exporter"
	"github.com/xz1220/github-radar/internal/service/jobs"
	"github.com/xz1220/github-radar/internal/service/snapshot"
	"github.com/xz1220/github-radar/internal/service/watch"
)

func (runtime *Runtime) Snapshot(ctx context.Context, dryRun bool) (SnapshotCommandReport, error) {
	if dryRun {
		targetCount, err := runtime.activeRepositoryCount(ctx)
		return SnapshotCommandReport{Report: snapshot.Report{
			Date:        domain.ShanghaiDate(runtime.now()),
			TargetCount: targetCount,
			Failures:    []snapshot.Failure{},
		}, DryRun: true}, err
	}
	if err := runtime.ensureTopics(ctx); err != nil {
		return SnapshotCommandReport{}, err
	}
	_, githubClient, _, err := runtime.dependencies()
	if err != nil {
		return SnapshotCommandReport{}, err
	}
	execution, err := runtime.tracker().Start(ctx, "snapshot", nil)
	if err != nil {
		return SnapshotCommandReport{}, err
	}
	report, runErr := runtime.snapshotService(githubClient).Run(ctx, nil)
	commandReport := SnapshotCommandReport{Report: report}
	status := domain.JobSuccess
	errorSummary := ""
	if runErr != nil {
		status = domain.JobFailed
		errorSummary = runErr.Error()
	} else if report.FailureCount > 0 {
		status = domain.JobPartial
		errorSummary = fmt.Sprintf("%d repository snapshots failed", report.FailureCount)
	}
	finishErr := execution.Finish(context.WithoutCancel(ctx), jobs.Outcome{
		Status:       status,
		TargetCount:  report.TargetCount,
		SuccessCount: report.SuccessCount,
		FailureCount: report.FailureCount,
		SkippedCount: report.SkippedCount,
		Details: map[string]any{
			"date": report.Date, "failures": report.Failures,
			"rate_limits": githubClient.RateLimits(),
		},
		ErrorSummary: errorSummary,
	})
	if runErr != nil {
		return commandReport, errors.Join(runErr, finishErr)
	}
	return commandReport, finishErr
}

func (runtime *Runtime) ListTopics(ctx context.Context) ([]domain.Topic, error) {
	if err := runtime.ensureTopics(ctx); err != nil {
		return nil, err
	}
	return runtime.topicService().List(ctx)
}

func (runtime *Runtime) AssignTopic(ctx context.Context, fullName, slug string, dryRun bool) (domain.TopicAssignmentResult, error) {
	if dryRun {
		repository, err := runtime.planningRepositoryByFullName(ctx, fullName)
		if err != nil {
			return domain.TopicAssignmentResult{}, err
		}
		topicValue, err := runtime.planningTopicBySlug(ctx, slug)
		if err != nil {
			return domain.TopicAssignmentResult{}, err
		}
		return plannedAssignment(repository, topicValue, runtime.now()), nil
	}
	if !dryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return domain.TopicAssignmentResult{}, err
		}
	}
	return runtime.topicService().Assign(ctx, fullName, slug, dryRun)
}

func (runtime *Runtime) RemoveTopic(ctx context.Context, fullName, slug string, dryRun bool) (bool, error) {
	if dryRun {
		if _, err := runtime.planningRepositoryByFullName(ctx, fullName); err != nil {
			return false, err
		}
		if _, err := runtime.planningTopicBySlug(ctx, slug); err != nil {
			return false, err
		}
		return true, nil
	}
	if !dryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return false, err
		}
	}
	return runtime.topicService().Remove(ctx, fullName, slug, dryRun)
}

func (runtime *Runtime) WatchAdd(ctx context.Context, fullName, note string, focus, dryRun bool) (watch.AddResult, error) {
	if !dryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return watch.AddResult{}, err
		}
	}
	_, githubClient, _, err := runtime.dependencies()
	if err != nil {
		return watch.AddResult{}, err
	}
	return runtime.watchService(githubClient).Add(ctx, fullName, note, focus, dryRun)
}

func (runtime *Runtime) WatchSet(ctx context.Context, fullName string, status domain.MonitoringStatus, dryRun bool) (domain.Repository, error) {
	if dryRun {
		if status != domain.MonitoringActive && status != domain.MonitoringPaused {
			return domain.Repository{}, fmt.Errorf("watch status must be active or paused")
		}
		repository, err := runtime.planningRepositoryByFullName(ctx, fullName)
		if err != nil {
			return domain.Repository{}, err
		}
		repository.MonitoringStatus = status
		return repository, nil
	}
	if !dryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return domain.Repository{}, err
		}
	}
	return runtime.watchService(nil).SetMonitoring(ctx, fullName, status, dryRun)
}

func (runtime *Runtime) Export(ctx context.Context, options ExportOptions) (ExportReport, error) {
	if options.Format != "csv" && options.Format != "json" && options.Format != "sqlite" {
		return ExportReport{}, fmt.Errorf("export format must be csv, json, or sqlite")
	}
	directory := firstNonEmpty(options.Directory, runtime.settings.ExportDirectory)
	if options.DryRun {
		return ExportReport{
			Result:      exporter.Result{Format: options.Format},
			DryRun:      true,
			PlannedPath: filepath.Clean(directory),
		}, nil
	}
	if err := runtime.ensureTopics(ctx); err != nil {
		return ExportReport{}, err
	}
	execution, err := runtime.tracker().Start(ctx, "export", map[string]any{"format": options.Format})
	if err != nil {
		return ExportReport{}, err
	}
	result, exportErr := runtime.dataExporter().Export(ctx, options.Format, directory)
	report := ExportReport{Result: result}
	status := domain.JobSuccess
	errorSummary := ""
	if exportErr != nil {
		status = domain.JobFailed
		errorSummary = exportErr.Error()
	}
	finishErr := execution.Finish(context.WithoutCancel(ctx), jobs.Outcome{
		Status:       status,
		TargetCount:  result.RepositoryCount + result.SnapshotCount,
		SuccessCount: result.RepositoryCount + result.SnapshotCount,
		Details:      result,
		ErrorSummary: errorSummary,
	})
	if exportErr != nil {
		return report, errors.Join(exportErr, finishErr)
	}
	return report, finishErr
}

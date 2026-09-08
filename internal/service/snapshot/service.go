package snapshot

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/source/github"
	corestore "github.com/xz1220/repotempo/internal/store"
)

type Store interface {
	ListRepositories(context.Context, domain.RepositoryFilter) ([]domain.Repository, error)
	GetDailySnapshot(context.Context, int64, domain.Date) (domain.DailySnapshot, error)
	GetLatestSuccessfulSnapshot(context.Context, int64, domain.Date) (domain.DailySnapshot, error)
	PutDailySnapshot(context.Context, domain.DailySnapshot) (domain.SnapshotWriteResult, error)
	UpsertRepository(context.Context, domain.RepositoryObservation) (domain.Repository, bool, error)
	SetRepositoryMonitoringStatus(context.Context, int64, domain.MonitoringStatus) error
	SetRepositoryGitHubStatus(context.Context, int64, domain.GitHubStatus) error
}

type RepositoryFetcher interface {
	FetchRepositoryByID(context.Context, int64, string) (github.RepositoryResult, error)
}

type OSSEvidence struct {
	TodayRank   *int
	WindowStars *int64
	TotalScore  *float64
}

type Failure struct {
	RepositoryID int64  `json:"repository_id"`
	FullName     string `json:"full_name"`
	ErrorCode    string `json:"error_code"`
	HTTPStatus   *int   `json:"http_status,omitempty"`
	Message      string `json:"message"`
}

type Report struct {
	Date                 domain.Date `json:"date"`
	TargetCount          int         `json:"target_count"`
	SuccessCount         int         `json:"success_count"`
	FailureCount         int         `json:"failure_count"`
	MetadataFailureCount int         `json:"metadata_failure_count"`
	SkippedCount         int         `json:"skipped_count"`
	Failures             []Failure   `json:"failures"`
}

type Service struct {
	Store  Store
	GitHub RepositoryFetcher
	Now    func() time.Time
}

func (service Service) Run(ctx context.Context, evidence map[int64]OSSEvidence) (Report, error) {
	if service.Store == nil || service.GitHub == nil {
		return Report{}, errors.New("snapshot service requires store and GitHub client")
	}
	if service.Now == nil {
		service.Now = time.Now
	}
	now := service.Now().UTC()
	date := domain.ShanghaiDate(now)
	repositories, err := service.Store.ListRepositories(ctx, domain.RepositoryFilter{MonitoringStatus: domain.MonitoringActive})
	if err != nil {
		return Report{}, fmt.Errorf("list active repositories: %w", err)
	}
	report := Report{Date: date, TargetCount: len(repositories), Failures: []Failure{}}
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		if existing, err := service.Store.GetDailySnapshot(ctx, repository.GitHubRepoID, date); err == nil && existing.FetchStatus == domain.FetchSuccess {
			report.SkippedCount++
			continue
		} else if err != nil && !errors.Is(err, corestore.ErrNotFound) {
			service.appendPersistenceFailure(&report, repository, "snapshot_lookup_failed", err)
			continue
		}

		etag, etagErr := service.safeETag(ctx, repository, date)
		if etagErr != nil {
			service.appendPersistenceFailure(&report, repository, "snapshot_baseline_failed", etagErr)
			continue
		}
		result, fetchErr := service.GitHub.FetchRepositoryByID(ctx, repository.GitHubRepoID, etag)
		if fetchErr != nil {
			failure := classifyFailure(repository, result, fetchErr)
			if putErr := service.putFailure(ctx, now, date, repository.GitHubRepoID, evidence[repository.GitHubRepoID], failure); putErr != nil {
				failure.ErrorCode = "snapshot_persist_failed"
				failure.Message = putErr.Error()
			}
			service.updateFailureState(ctx, repository.GitHubRepoID, fetchErr)
			report.FailureCount++
			report.Failures = append(report.Failures, failure)
			if github.IsAccessBlocked(fetchErr) {
				// Record only the response we actually received. Repositories that
				// have not been requested remain missing, not synthetic failures.
				return report, fmt.Errorf("snapshot batch stopped after GitHub access failure for %s: %w", repository.FullName, fetchErr)
			}
			continue
		}

		starCount, successErr := service.starCountForResult(ctx, repository, date, result)
		if successErr != nil {
			failure := classifyFailure(repository, result, successErr)
			if putErr := service.putFailure(ctx, now, date, repository.GitHubRepoID, evidence[repository.GitHubRepoID], failure); putErr != nil {
				failure.ErrorCode = "snapshot_persist_failed"
				failure.Message = putErr.Error()
			}
			report.FailureCount++
			report.Failures = append(report.Failures, failure)
			continue
		}

		snapshot := domain.DailySnapshot{
			RepositoryID: repository.GitHubRepoID,
			SnapshotDate: date,
			CapturedAt:   now,
			StarCount:    &starCount,
			FetchStatus:  domain.FetchSuccess,
		}
		if result.HTTPStatus > 0 {
			httpStatus := result.HTTPStatus
			snapshot.HTTPStatus = &httpStatus
		}
		attachEvidence(&snapshot, evidence[repository.GitHubRepoID])
		write, err := service.Store.PutDailySnapshot(ctx, snapshot)
		if err != nil {
			service.appendPersistenceFailure(&report, repository, "snapshot_persist_failed", err)
			continue
		}
		if write.Disposition == domain.SnapshotSuccessProtected {
			report.SkippedCount++
		} else {
			report.SuccessCount++
		}
		// Persist ETag/name/status only after the corresponding star value is
		// durable. A failed snapshot write must never advance the condition used
		// by tomorrow's request.
		if err := service.updateRepository(ctx, repository, result, now); err != nil {
			report.MetadataFailureCount++
			report.Failures = append(report.Failures, Failure{
				RepositoryID: repository.GitHubRepoID,
				FullName:     repository.FullName,
				ErrorCode:    "repository_update_failed",
				Message:      err.Error(),
			})
		}
	}
	return report, nil
}

func (service Service) appendPersistenceFailure(report *Report, repository domain.Repository, code string, err error) {
	report.FailureCount++
	report.Failures = append(report.Failures, Failure{
		RepositoryID: repository.GitHubRepoID,
		FullName:     repository.FullName,
		ErrorCode:    code,
		Message:      err.Error(),
	})
}

func (service Service) safeETag(ctx context.Context, repository domain.Repository, date domain.Date) (string, error) {
	if repository.GitHubETag == "" || repository.GitHubTopics == nil {
		return "", nil
	}
	previousDate, err := date.AddDays(-1)
	if err != nil {
		return "", err
	}
	_, err = service.Store.GetLatestSuccessfulSnapshot(ctx, repository.GitHubRepoID, previousDate)
	if errors.Is(err, corestore.ErrNotFound) {
		// An imported metadata ETag without an absolute-star baseline could
		// produce a 304 whose value cannot be reconstructed. Force one full
		// response before conditional requests begin.
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return repository.GitHubETag, nil
}

func (service Service) starCountForResult(ctx context.Context, repository domain.Repository, date domain.Date, result github.RepositoryResult) (int64, error) {
	if result.NotModified {
		previousDate, err := date.AddDays(-1)
		if err != nil {
			return 0, err
		}
		previous, err := service.Store.GetLatestSuccessfulSnapshot(ctx, repository.GitHubRepoID, previousDate)
		if err != nil {
			if errors.Is(err, corestore.ErrNotFound) {
				return 0, errors.New("not_modified_without_successful_baseline")
			}
			return 0, err
		}
		if previous.StarCount == nil {
			return 0, errors.New("successful baseline has null star count")
		}
		return *previous.StarCount, nil
	}
	if result.Repository.AbsoluteStars == nil {
		return 0, errors.New("github response omitted absolute star count")
	}
	return *result.Repository.AbsoluteStars, nil
}

func (service Service) updateRepository(ctx context.Context, existing domain.Repository, result github.RepositoryResult, checkedAt time.Time) error {
	observation := domain.RepositoryObservation{
		GitHubRepoID:     existing.GitHubRepoID,
		GitHubNodeID:     existing.GitHubNodeID,
		FullName:         existing.FullName,
		HTMLURL:          existing.HTMLURL,
		Source:           existing.FirstSeenSource,
		Profile:          existing.FirstSeenProfile,
		DiscoveredAt:     existing.LastDiscoveredAt,
		MonitoringStatus: existing.MonitoringStatus,
		GitHubStatus:     existing.GitHubStatus,
		GitHubETag:       &result.ETag,
		LastCheckedAt:    &checkedAt,
	}
	if !result.NotModified {
		repository := result.Repository
		observation.GitHubNodeID = repository.NodeID
		observation.FullName = repository.FullName
		observation.HTMLURL = repository.HTMLURL
		observation.Description = &repository.Description
		observation.PrimaryLanguage = &repository.Language
		observation.GitHubCreatedAt = repository.CreatedAt
		if repository.Topics != nil {
			observation.GitHubTopics = &repository.Topics
		}
		switch {
		case repository.Private:
			observation.GitHubStatus = domain.GitHubPrivate
		case repository.Archived:
			observation.GitHubStatus = domain.GitHubArchived
		default:
			observation.GitHubStatus = domain.GitHubActive
		}
	}
	_, _, err := service.Store.UpsertRepository(ctx, observation)
	return err
}

func (service Service) putFailure(ctx context.Context, now time.Time, date domain.Date, repositoryID int64, evidence OSSEvidence, failure Failure) error {
	snapshot := domain.DailySnapshot{
		RepositoryID: repositoryID,
		SnapshotDate: date,
		CapturedAt:   now,
		FetchStatus:  domain.FetchFailed,
		HTTPStatus:   failure.HTTPStatus,
		ErrorCode:    failure.ErrorCode,
	}
	attachEvidence(&snapshot, evidence)
	_, err := service.Store.PutDailySnapshot(ctx, snapshot)
	return err
}

func attachEvidence(snapshot *domain.DailySnapshot, evidence OSSEvidence) {
	snapshot.OSSTodayRank = evidence.TodayRank
	snapshot.OSSWindowStars = evidence.WindowStars
	snapshot.OSSTotalScore = evidence.TotalScore
}

func classifyFailure(repository domain.Repository, result github.RepositoryResult, err error) Failure {
	failure := Failure{RepositoryID: repository.GitHubRepoID, FullName: repository.FullName, ErrorCode: "snapshot_error", Message: err.Error()}
	if result.HTTPStatus > 0 {
		status := result.HTTPStatus
		failure.HTTPStatus = &status
	}
	var apiError *github.APIError
	if errors.As(err, &apiError) {
		failure.ErrorCode = string(apiError.Code)
		if apiError.StatusCode > 0 {
			status := apiError.StatusCode
			failure.HTTPStatus = &status
		}
		return failure
	}
	var transportError *github.TransportError
	if errors.As(err, &transportError) {
		failure.ErrorCode = string(transportError.Code)
		return failure
	}
	var mismatch *github.RepositoryIDMismatchError
	if errors.As(err, &mismatch) {
		failure.ErrorCode = string(github.CodeRepositoryIDMismatch)
		return failure
	}
	switch err.Error() {
	case "not_modified_without_successful_baseline":
		failure.ErrorCode = "not_modified_without_baseline"
	case "github response omitted absolute star count":
		failure.ErrorCode = "missing_star_count"
	}
	return failure
}

func (service Service) updateFailureState(ctx context.Context, repositoryID int64, err error) {
	var mismatch *github.RepositoryIDMismatchError
	if errors.As(err, &mismatch) {
		_ = service.Store.SetRepositoryMonitoringStatus(ctx, repositoryID, domain.MonitoringStopped)
		_ = service.Store.SetRepositoryGitHubStatus(ctx, repositoryID, domain.GitHubUnreachable)
		return
	}
	var apiError *github.APIError
	if errors.As(err, &apiError) && apiError.StatusCode == 404 {
		_ = service.Store.SetRepositoryGitHubStatus(ctx, repositoryID, domain.GitHubUnreachable)
	}
}

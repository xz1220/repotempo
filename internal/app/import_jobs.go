package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/classification"
	"github.com/xz1220/repotempo/internal/service/watch"
	"github.com/xz1220/repotempo/internal/source/github"
	corestore "github.com/xz1220/repotempo/internal/store"
)

func (runtime *Runtime) EnqueueImport(ctx context.Context, request domain.RepositoryImportRequest) (domain.RepositoryImportJob, error) {
	if principal, scoped := domain.PrincipalFromContext(ctx); scoped {
		if !principal.Admin || principal.UserID <= 0 {
			return domain.RepositoryImportJob{}, domain.ErrImportInvalid
		}
		request.OwnerUserID = principal.UserID
	}
	name, err := watch.NormalizeRepository(request.Repository)
	if err != nil || !utf8.ValidString(request.Note) || utf8.RuneCountInString(request.Note) > 2000 || len(request.TopicSlug) > 100 {
		return domain.RepositoryImportJob{}, domain.ErrImportInvalid
	}
	request.Repository = name
	request.Note, request.TopicSlug = strings.TrimSpace(request.Note), strings.TrimSpace(request.TopicSlug)
	if request.TopicSlug != "" {
		topic, err := runtime.store.GetTopicBySlug(ctx, request.TopicSlug)
		if err != nil || topic.Status != domain.TopicActive {
			return domain.RepositoryImportJob{}, domain.ErrImportInvalid
		}
	}
	job, err := runtime.store.EnqueueRepositoryImport(ctx, request)
	if err == nil {
		runtime.importMu.Lock()
		wake := runtime.importWake
		runtime.importMu.Unlock()
		if wake != nil {
			select {
			case wake <- struct{}{}:
			default:
			}
		}
	}
	return job, err
}

func (runtime *Runtime) GetImport(ctx context.Context, id string) (domain.RepositoryImportJob, error) {
	if principal, scoped := domain.PrincipalFromContext(ctx); scoped && !principal.Admin {
		return domain.RepositoryImportJob{}, corestore.ErrNotFound
	}
	return runtime.store.GetRepositoryImport(ctx, id)
}

// RunImportWorker runs until cancellation, independently of request contexts.
// A durable lease serializes processing across concurrent application instances.
func (runtime *Runtime) RunImportWorker(ctx context.Context) error {
	workerContext, done, err := runtime.prepareImportWorker(ctx)
	if err != nil {
		return err
	}
	runtime.importLoop(workerContext, done)
	return nil
}

func (runtime *Runtime) prepareImportWorker(ctx context.Context) (context.Context, chan struct{}, error) {
	runtime.importMu.Lock()
	defer runtime.importMu.Unlock()
	if runtime.importDone != nil || runtime.importClosed {
		return nil, nil, domain.ErrImportBusy
	}
	workerContext, cancel := context.WithCancel(ctx)
	runtime.importCancel, runtime.importDone = cancel, make(chan struct{})
	if runtime.importWake == nil {
		runtime.importWake = make(chan struct{}, 1)
	}
	return workerContext, runtime.importDone, nil
}

func (runtime *Runtime) startImportWorker(ctx context.Context) error {
	workerContext, done, err := runtime.prepareImportWorker(ctx)
	if err != nil {
		return err
	}
	go runtime.importLoop(workerContext, done)
	return nil
}

func (runtime *Runtime) stopImportWorker() {
	runtime.importMu.Lock()
	cancel, done := runtime.importCancel, runtime.importDone
	runtime.importMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (runtime *Runtime) importLoop(ctx context.Context, done chan struct{}) {
	defer func() {
		runtime.importMu.Lock()
		if runtime.importDone == done {
			runtime.importCancel()
			runtime.importCancel, runtime.importDone = nil, nil
		}
		close(done)
		runtime.importMu.Unlock()
	}()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	for ctx.Err() == nil {
		job, token, err := runtime.store.ClaimRepositoryImport(ctx, runtime.now(), 2*time.Minute)
		if err == nil && job != nil {
			runtime.processImport(ctx, *job, token)
			continue
		}
		if err != nil && ctx.Err() == nil && runtime.logger != nil {
			runtime.logger.WarnContext(ctx, "import queue unavailable")
		}
		select {
		case <-ctx.Done():
			return
		case <-runtime.importWake:
		case <-timer.C:
		}
	}
}

func (runtime *Runtime) processImport(parent context.Context, job domain.RepositoryImportJob, token string) {
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	checkpoint := func() bool {
		err := runtime.store.UpdateRepositoryImport(ctx, job, token)
		if err != nil && runtime.logger != nil {
			runtime.logger.WarnContext(ctx, "import progress not saved", "run_id", job.ID)
		}
		return err == nil
	}
	_, client, _, err := runtime.dependencies()
	if err != nil {
		runtime.failImport(parent, job, token, "configuration_unavailable", false, 0)
		return
	}
	job.ErrorCode = ""
	var repository domain.Repository
	if job.RepositoryID == 0 {
		service := watch.Service{Store: runtime.store, Resolver: client, Now: runtime.now}
		note, focus := job.Request.Note, job.Request.Focus
		if job.Request.OwnerUserID > 0 {
			note, focus = "", false
		}
		result, err := service.ImportTracked(ctx, job.Request.Repository, note, job.Request.TopicSlug, focus)
		if err != nil {
			code, retry, delay := importError(err, "metadata")
			runtime.failImport(parent, job, token, code, retry, delay)
			return
		}
		repository = result.Repository
		if job.Request.OwnerUserID > 0 && (job.Request.Focus || job.Request.Note != "") {
			personalErr := runtime.store.MergeUserRepositoryState(ctx, job.Request.OwnerUserID, repository.GitHubRepoID, job.Request.Focus, job.Request.Note)
			if personalErr != nil {
				runtime.failImport(parent, job, token, "storage_unavailable", true, 0)
				return
			}
		}
		job.RepositoryID, job.FullName, job.Created = repository.GitHubRepoID, repository.FullName, result.Created
		job.Stage = "reading"
		if !checkpoint() {
			return
		}
	} else {
		repository, err = runtime.store.GetRepository(ctx, job.RepositoryID)
		if err != nil {
			runtime.failImport(parent, job, token, "storage_unavailable", true, 0)
			return
		}
	}
	if job.Readme == nil {
		readme, err := client.FetchReadme(ctx, job.RepositoryID, repository.FullName)
		if err != nil {
			code, retry, delay := importError(err, "readme")
			if retry || parent.Err() != nil {
				runtime.failImport(parent, job, token, code, retry, delay)
				return
			}
			// Missing/invalid README does not discard the already imported repo.
			job.ErrorCode = code
		} else {
			job.Readme = &readme
		}
	}
	job.Stage = "classifying"
	if !checkpoint() {
		return
	}
	if job.Request.TopicSlug == "" {
		if _, err := classification.Apply(ctx, runtime.store, repository, repository.GitHubTopics, runtime.now()); err != nil {
			runtime.failImport(parent, job, token, "classification_failed", true, 0)
			return
		}
	}
	job.Stage = "done"
	if job.ErrorCode != "" {
		job.Stage = "partial"
	}
	checkpoint()
}

// Persist only bounded error categories, never raw GitHub errors or settings.
func importError(err error, phase string) (code string, retry bool, delay time.Duration) {
	var api *github.APIError
	var identity *github.RepositoryIDMismatchError
	switch {
	case errors.Is(err, watch.ErrInvalidRepository), errors.Is(err, watch.ErrMissingStars), errors.Is(err, corestore.ErrInvalid):
		return "invalid", false, 0
	case errors.Is(err, watch.ErrPrivateRepository):
		return "not_found_or_private", false, 0
	case errors.As(err, &identity), errors.Is(err, corestore.ErrRepositoryIdentityMismatch):
		return "identity_mismatch", false, 0
	case errors.As(err, &api):
		switch api.Code {
		case github.CodePrimaryRateLimit, github.CodeSecondaryRateLimit:
			return "rate_limited", true, api.RateLimit.RetryAfter
		case github.CodeNotFoundOrPrivate:
			if phase == "readme" {
				return "readme_unavailable", false, 0
			}
			return "not_found_or_private", false, 0
		case github.CodeForbidden, github.CodeBadRequest, github.CodeValidation:
			return "invalid", false, 0
		}
		return "upstream_unavailable", api.Temporary(), 0
	}
	var transport *github.TransportError
	if errors.As(err, &transport) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "upstream_unavailable", true, 0
	}
	if phase == "readme" {
		return "readme_invalid", false, 0
	}
	return "upstream_unavailable", true, 0
}

func (runtime *Runtime) failImport(parent context.Context, job domain.RepositoryImportJob, token, code string, retry bool, delay time.Duration) {
	now := runtime.now().UTC()
	job.ErrorCode = code
	job.Stage = "failed"
	if job.RepositoryID > 0 {
		job.Stage = "partial"
	}
	if parent.Err() != nil {
		code, retry, delay = "interrupted", true, 0
		job.ErrorCode = code
	}
	if retry && job.Attempts < domain.ImportMaxAttempts {
		if parent.Err() == nil && delay < time.Duration(job.Attempts)*time.Minute {
			delay = time.Duration(job.Attempts) * time.Minute
		}
		// A rate response can be followed by a timeout while c.do respects the
		// reset. Do not immediately requeue against a known exhausted budget.
		runtime.dependencyMu.Lock()
		client := runtime.github
		runtime.dependencyMu.Unlock()
		if client != nil && parent.Err() == nil {
			rate := client.RateLimits()[github.ResourceCore]
			if rate.Remaining == 0 && rate.Reset.After(now.Add(delay)) {
				delay = rate.Reset.Sub(now) + time.Second
			}
		}
		next := now.Add(delay)
		job.Stage, job.NextAttemptAt = "queued", &next
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), 3*time.Second)
	defer cancel()
	if err := runtime.store.UpdateRepositoryImport(cleanup, job, token); err != nil && runtime.logger != nil {
		runtime.logger.WarnContext(cleanup, "import retry status not saved", "run_id", job.ID)
	}
}

package app

import (
	"context"
	"errors"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
	"github.com/xz1220/repotempo/internal/web"
)

var _ web.Importer = (*Runtime)(nil)

func (runtime *Runtime) SubmitImport(ctx context.Context, request web.WatchRequest) (web.ImportStatus, error) {
	job, err := runtime.EnqueueImport(ctx, domain.RepositoryImportRequest{Repository: request.Repository, Note: request.Note, TopicSlug: request.TopicSlug, Focus: request.Focus})
	if errors.Is(err, domain.ErrImportInvalid) {
		return web.ImportStatus{}, web.ErrWatchInvalid
	}
	if errors.Is(err, domain.ErrImportBusy) {
		return web.ImportStatus{}, web.ErrImportQueueBusy
	}
	if err != nil {
		return web.ImportStatus{}, web.ErrWatchUnavailable
	}
	return mapImportStatus(job), nil
}

func (runtime *Runtime) GetImportStatus(ctx context.Context, id string) (web.ImportStatus, error) {
	job, err := runtime.GetImport(ctx, id)
	if errors.Is(err, corestore.ErrNotFound) {
		return web.ImportStatus{}, web.ErrNotFound
	}
	if err != nil {
		return web.ImportStatus{}, web.ErrWatchUnavailable
	}
	return mapImportStatus(job), nil
}

func mapImportStatus(job domain.RepositoryImportJob) web.ImportStatus {
	return web.ImportStatus{ID: job.ID, Repository: job.Request.Repository, FullName: job.FullName, Stage: job.Stage,
		RepositoryID: job.RepositoryID, Focus: job.Request.Focus, Created: job.Created, Attempts: job.Attempts,
		QueuedAt: job.QueuedAt, UpdatedAt: job.UpdatedAt, NextAttemptAt: job.NextAttemptAt, ErrorCode: job.ErrorCode,
		Readme: mapImportReadme(job.Readme)}
}

func mapImportReadme(value *domain.RepositoryReadme) *web.RepositoryReadme {
	if value == nil {
		return nil
	}
	return &web.RepositoryReadme{Intro: value.Intro, HTMLURL: value.HTMLURL, SHA: value.SHA, Path: value.Path,
		Headings: append([]string(nil), value.Headings...), FetchedAt: value.FetchedAt, Truncated: value.Truncated}
}

func (adapter WebAdapter) addImportReadmes(ctx context.Context, items []web.RepositoryMetric) error {
	reader, ok := adapter.Store.(interface {
		LatestImportReadmes(context.Context, []int64) (map[int64]*domain.RepositoryReadme, error)
	})
	if !ok || len(items) == 0 {
		return nil
	}
	ids := make([]int64, len(items))
	for index := range items {
		ids[index] = items[index].ID
	}
	readmes, err := reader.LatestImportReadmes(ctx, ids)
	if err != nil {
		return err
	}
	for index := range items {
		items[index].Readme = mapImportReadme(readmes[items[index].ID])
	}
	return nil
}

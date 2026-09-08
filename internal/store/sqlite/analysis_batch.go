package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

var _ corestore.AnalysisBatchStore = (*Store)(nil)

func (store *Store) ImportRepositoryAnalyses(ctx context.Context, projects []domain.AnalysisImportProject) (domain.AnalysisBatchResult, error) {
	if len(projects) == 0 || len(projects) > domain.MaxAnalysisBatchProjects {
		return domain.AnalysisBatchResult{}, fmt.Errorf("%w: analysis batch requires 1 to %d projects", corestore.ErrInvalid, domain.MaxAnalysisBatchProjects)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.AnalysisBatchResult{}, fmt.Errorf("begin analysis batch: %w", err)
	}
	defer tx.Rollback()
	lookup, err := tx.PrepareContext(ctx, `SELECT r.github_repo_id, a.summary_zh
FROM repositories r LEFT JOIN repository_analyses a ON a.repository_id = r.github_repo_id
WHERE r.full_name = ? COLLATE NOCASE`)
	if err != nil {
		return domain.AnalysisBatchResult{}, fmt.Errorf("prepare analysis batch lookup: %w", err)
	}
	defer lookup.Close()
	write, err := tx.PrepareContext(ctx, repositoryAnalysisUpsertSQL)
	if err != nil {
		return domain.AnalysisBatchResult{}, fmt.Errorf("prepare analysis batch write: %w", err)
	}
	defer write.Close()
	seen := make(map[int64]struct{}, len(projects))
	result := domain.AnalysisBatchResult{}
	now := store.nowUTC()
	for index, project := range projects {
		name := strings.TrimSpace(project.FullName)
		if name == "" {
			return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d requires full_name", corestore.ErrInvalid, index+1)
		}
		var repositoryID int64
		var existingSummary sql.NullString
		if err := lookup.QueryRowContext(ctx, name).Scan(&repositoryID, &existingSummary); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d (%s) is not in the registry", corestore.ErrNotFound, index+1, name)
			}
			return domain.AnalysisBatchResult{}, fmt.Errorf("lookup analysis batch project %d: %w", index+1, err)
		}
		if project.RepositoryID != nil && (*project.RepositoryID <= 0 || *project.RepositoryID != repositoryID) {
			return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d (%s) has a different repository ID", corestore.ErrRepositoryIdentityMismatch, index+1, name)
		}
		if _, duplicate := seen[repositoryID]; duplicate {
			return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d (%s) duplicates repository ID %d", corestore.ErrInvalid, index+1, name, repositoryID)
		}
		seen[repositoryID] = struct{}{}
		if !project.AnalyzedAt.IsZero() {
			if _, err := time.Parse(time.RFC3339Nano, storedTime(project.AnalyzedAt)); err != nil {
				return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d analyzed_at is invalid", corestore.ErrInvalid, index+1)
			}
		}
		_, arguments, err := prepareRepositoryAnalysis(domain.RepositoryAnalysis{
			RepositoryID: repositoryID, SummaryZH: project.SummaryZH, KeyPoints: project.KeyPoints,
			UseCases: project.UseCases, TechnicalNotes: project.TechnicalNotes,
			Source: project.Source, Model: project.Model, AnalyzedAt: project.AnalyzedAt,
		}, now)
		if err != nil {
			return domain.AnalysisBatchResult{}, fmt.Errorf("%w: project %d (%s): %v", corestore.ErrInvalid, index+1, name, err)
		}
		// Validate even skipped entries. An invalid payload must never be
		// reported as a successful batch simply because its target was filled.
		if existingSummary.Valid && strings.TrimSpace(existingSummary.String) != "" {
			result.Skipped++
			continue
		}
		if _, err := write.ExecContext(ctx, arguments...); err != nil {
			return domain.AnalysisBatchResult{}, fmt.Errorf("write analysis batch project %d (%s): %w", index+1, name, err)
		}
		result.Imported++
	}
	if err := tx.Commit(); err != nil {
		return domain.AnalysisBatchResult{}, fmt.Errorf("commit analysis batch: %w", err)
	}
	return result, nil
}

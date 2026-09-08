package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

const repositoryAnalysisColumns = `
repository_id, summary_zh, key_points_json, use_cases_json, technical_notes,
source, model, revision, analyzed_at, created_at, updated_at`

func scanRepositoryAnalysis(row scanner) (domain.RepositoryAnalysis, error) {
	var analysis domain.RepositoryAnalysis
	var keyPointsJSON string
	var useCasesJSON string
	var analyzedAt string
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&analysis.RepositoryID,
		&analysis.SummaryZH,
		&keyPointsJSON,
		&useCasesJSON,
		&analysis.TechnicalNotes,
		&analysis.Source,
		&analysis.Model,
		&analysis.Revision,
		&analyzedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.RepositoryAnalysis{}, err
	}

	if err := json.Unmarshal([]byte(keyPointsJSON), &analysis.KeyPoints); err != nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("decode repository analysis key points: %w", err)
	}
	if err := json.Unmarshal([]byte(useCasesJSON), &analysis.UseCases); err != nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("decode repository analysis use cases: %w", err)
	}
	var err error
	analysis.AnalyzedAt, err = parseStoredTime(analyzedAt)
	if err != nil {
		return domain.RepositoryAnalysis{}, err
	}
	analysis.CreatedAt, err = parseStoredTime(createdAt)
	if err != nil {
		return domain.RepositoryAnalysis{}, err
	}
	analysis.UpdatedAt, err = parseStoredTime(updatedAt)
	if err != nil {
		return domain.RepositoryAnalysis{}, err
	}
	return analysis, nil
}

func (store *Store) getRepositoryAnalysis(ctx context.Context, repositoryID int64) (*domain.RepositoryAnalysis, error) {
	analysis, err := scanRepositoryAnalysis(store.db.QueryRowContext(ctx,
		"SELECT "+repositoryAnalysisColumns+" FROM repository_analyses WHERE repository_id = ?",
		repositoryID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read repository analysis: %w", err)
	}
	return &analysis, nil
}

// loadRepositoryAnalysesByIDs reads the bounded feed page in one query. A
// missing record remains nil; incomplete saved summaries are not synthesized
// from GitHub descriptions or manual notes.
func (store *Store) loadRepositoryAnalysesByIDs(ctx context.Context, repositoryIDs []int64) (map[int64]*domain.RepositoryAnalysis, error) {
	result := make(map[int64]*domain.RepositoryAnalysis, len(repositoryIDs))
	if len(repositoryIDs) == 0 {
		return result, nil
	}
	rows, err := store.db.QueryContext(ctx,
		"SELECT "+repositoryAnalysisColumns+" FROM repository_analyses WHERE repository_id IN ("+placeholders(len(repositoryIDs))+")",
		anyIDs(repositoryIDs)...,
	)
	if err != nil {
		return nil, fmt.Errorf("load feed repository analyses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		analysis, err := scanRepositoryAnalysis(rows)
		if err != nil {
			return nil, fmt.Errorf("scan feed repository analysis: %w", err)
		}
		result[analysis.RepositoryID] = &analysis
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feed repository analyses: %w", err)
	}
	return result, nil
}

func prepareRepositoryAnalysis(analysis domain.RepositoryAnalysis, now time.Time) (domain.RepositoryAnalysis, []any, error) {
	analysis.SummaryZH = strings.TrimSpace(analysis.SummaryZH)
	analysis.Source = strings.TrimSpace(analysis.Source)
	analysis.Model = strings.TrimSpace(analysis.Model)
	analysis.TechnicalNotes = strings.TrimSpace(analysis.TechnicalNotes)
	if analysis.RepositoryID <= 0 {
		return domain.RepositoryAnalysis{}, nil, fmt.Errorf("repository analysis: repository ID must be positive")
	}
	if analysis.SummaryZH == "" {
		return domain.RepositoryAnalysis{}, nil, fmt.Errorf("repository analysis: Chinese summary is required")
	}
	if analysis.Source == "" {
		return domain.RepositoryAnalysis{}, nil, fmt.Errorf("repository analysis: source is required")
	}
	if analysis.KeyPoints == nil {
		analysis.KeyPoints = []string{}
	}
	if analysis.UseCases == nil {
		analysis.UseCases = []string{}
	}
	keyPoints, err := json.Marshal(analysis.KeyPoints)
	if err != nil {
		return domain.RepositoryAnalysis{}, nil, fmt.Errorf("encode repository analysis key points: %w", err)
	}
	useCases, err := json.Marshal(analysis.UseCases)
	if err != nil {
		return domain.RepositoryAnalysis{}, nil, fmt.Errorf("encode repository analysis use cases: %w", err)
	}
	now = now.UTC()
	if analysis.AnalyzedAt.IsZero() {
		analysis.AnalyzedAt = now
	} else {
		analysis.AnalyzedAt = analysis.AnalyzedAt.UTC()
	}
	return analysis, []any{
		analysis.RepositoryID, analysis.SummaryZH, string(keyPoints), string(useCases),
		analysis.TechnicalNotes, analysis.Source, analysis.Model,
		storedTime(analysis.AnalyzedAt), storedTime(now), storedTime(now),
	}, nil
}

const repositoryAnalysisUpsertSQL = `
INSERT INTO repository_analyses (
    repository_id, summary_zh, key_points_json, use_cases_json,
    technical_notes, source, model, revision, analyzed_at, created_at, updated_at
)
VALUES (?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?)
ON CONFLICT(repository_id) DO UPDATE SET
    summary_zh = excluded.summary_zh,
    key_points_json = excluded.key_points_json,
    use_cases_json = excluded.use_cases_json,
    technical_notes = excluded.technical_notes,
    source = excluded.source,
    model = excluded.model,
    revision = repository_analyses.revision + 1,
    analyzed_at = excluded.analyzed_at,
    updated_at = excluded.updated_at`

func (store *Store) PutRepositoryAnalysis(ctx context.Context, analysis domain.RepositoryAnalysis) (domain.RepositoryAnalysis, error) {
	analysis, arguments, err := prepareRepositoryAnalysis(analysis, store.nowUTC())
	if err != nil {
		return domain.RepositoryAnalysis{}, err
	}
	_, err = store.db.ExecContext(ctx, repositoryAnalysisUpsertSQL, arguments...)
	if err != nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("write repository analysis: %w", err)
	}
	stored, err := store.getRepositoryAnalysis(ctx, analysis.RepositoryID)
	if err != nil {
		return domain.RepositoryAnalysis{}, err
	}
	if stored == nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("write repository analysis: stored value disappeared")
	}
	return *stored, nil
}

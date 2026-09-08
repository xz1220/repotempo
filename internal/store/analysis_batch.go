package store

import (
	"context"

	"github.com/xz1220/repotempo/internal/domain"
)

// AnalysisBatchStore is optional so existing command and store test doubles
// remain compatible with the single-project import interface.
type AnalysisBatchStore interface {
	ImportRepositoryAnalyses(context.Context, []domain.AnalysisImportProject) (domain.AnalysisBatchResult, error)
}

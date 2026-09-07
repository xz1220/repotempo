package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
)

func (runtime *Runtime) ImportAnalysis(ctx context.Context, fullName string, analysis domain.RepositoryAnalysis) (domain.RepositoryAnalysis, error) {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return domain.RepositoryAnalysis{}, fmt.Errorf("analysis import: repository is required")
	}
	repository, err := runtime.store.GetRepositoryByFullName(ctx, fullName)
	if err != nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("analysis import for %s: %w", fullName, err)
	}
	analysis.RepositoryID = repository.GitHubRepoID
	if strings.TrimSpace(analysis.Source) == "" {
		analysis.Source = "codex"
	}
	stored, err := runtime.store.PutRepositoryAnalysis(ctx, analysis)
	if err != nil {
		return domain.RepositoryAnalysis{}, fmt.Errorf("analysis import for %s: %w", repository.FullName, err)
	}
	return stored, nil
}

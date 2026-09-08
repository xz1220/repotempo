package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

var _ corestore.TagsImportStore = (*Store)(nil)

func (store *Store) ImportRepositoryTags(ctx context.Context, projects []domain.RepositoryTagsImport) (domain.TagsBatchResult, error) {
	if len(projects) == 0 || len(projects) > 10000 {
		return domain.TagsBatchResult{}, fmt.Errorf("%w: tags batch requires 1 to 10000 projects", corestore.ErrInvalid)
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TagsBatchResult{}, err
	}
	defer tx.Rollback()
	lookup, err := tx.PrepareContext(ctx, `SELECT full_name, github_topics_json, research_tags_json FROM repositories WHERE github_repo_id=?`)
	if err != nil {
		return domain.TagsBatchResult{}, err
	}
	defer lookup.Close()
	write, err := tx.PrepareContext(ctx, `UPDATE repositories SET github_topics_json=?, research_tags_json=?, github_etag=CASE WHEN ? THEN '' ELSE github_etag END, updated_at=? WHERE github_repo_id=?`)
	if err != nil {
		return domain.TagsBatchResult{}, err
	}
	defer write.Close()
	seen := map[int64]bool{}
	seenNames := map[string]bool{}
	result := domain.TagsBatchResult{}
	for index, project := range projects {
		name := strings.TrimSpace(project.FullName)
		if project.RepositoryID <= 0 || name == "" || seen[project.RepositoryID] || seenNames[strings.ToLower(name)] {
			return domain.TagsBatchResult{}, fmt.Errorf("%w: tags project %d has missing or duplicate identity", corestore.ErrInvalid, index+1)
		}
		seen[project.RepositoryID], seenNames[strings.ToLower(name)] = true, true
		observation, err := normalizeObservationTags(domain.RepositoryObservation{GitHubTopics: project.GitHubTopics, ResearchTags: project.ResearchTags})
		if err != nil {
			return domain.TagsBatchResult{}, err
		}
		var storedName, researchJSON string
		var githubJSON sql.NullString
		if err := lookup.QueryRowContext(ctx, project.RepositoryID).Scan(&storedName, &githubJSON, &researchJSON); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return domain.TagsBatchResult{}, fmt.Errorf("%w: tags project %d not in registry", corestore.ErrNotFound, index+1)
			}
			return domain.TagsBatchResult{}, err
		}
		if !strings.EqualFold(storedName, name) {
			return domain.TagsBatchResult{}, fmt.Errorf("%w: tags project %d full_name does not match ID", corestore.ErrRepositoryIdentityMismatch, index+1)
		}
		var githubValue any
		if githubJSON.Valid {
			githubValue = githubJSON.String
		}
		filled := !githubJSON.Valid && observation.GitHubTopics != nil
		if filled {
			encoded, err := json.Marshal(*observation.GitHubTopics)
			if err != nil {
				return domain.TagsBatchResult{}, err
			}
			githubValue = string(encoded)
		}
		changed := filled
		added := 0
		if observation.ResearchTags != nil {
			var existing []string
			if err := json.Unmarshal([]byte(researchJSON), &existing); err != nil {
				return domain.TagsBatchResult{}, err
			}
			normalized, err := domain.NormalizeRepositoryTags(existing)
			if err != nil {
				return domain.TagsBatchResult{}, err
			}
			merged, err := domain.NormalizeRepositoryTags(append(slices.Clone(normalized), (*observation.ResearchTags)...))
			if err != nil {
				return domain.TagsBatchResult{}, err
			}
			if merged == nil {
				merged = []string{}
			}
			added = len(merged) - len(normalized)
			if !slices.Equal(existing, merged) {
				encoded, err := json.Marshal(merged)
				if err != nil {
					return domain.TagsBatchResult{}, err
				}
				researchJSON = string(encoded)
				changed = true
			}
		}
		if !changed {
			result.Skipped++
			continue
		}
		if _, err := write.ExecContext(ctx, githubValue, researchJSON, filled, storedTime(store.nowUTC()), project.RepositoryID); err != nil {
			return domain.TagsBatchResult{}, err
		}
		result.Updated++
		if filled {
			result.GitHubTopicsFilled++
		}
		result.ResearchTagsAdded += added
	}
	if err := tx.Commit(); err != nil {
		return domain.TagsBatchResult{}, err
	}
	return result, nil
}

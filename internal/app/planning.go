package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/xz1220/repotempo/internal/domain"
)

func (runtime *Runtime) activeRepositoryCount(ctx context.Context) (int, error) {
	if runtime.planningDB == nil {
		repositories, err := runtime.store.ListRepositories(ctx, domain.RepositoryFilter{MonitoringStatus: domain.MonitoringActive})
		return len(repositories), err
	}
	var count int
	if err := runtime.planningDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM repositories WHERE monitoring_status = 'active'`).Scan(&count); err != nil {
		return 0, fmt.Errorf("read active repository plan: %w", err)
	}
	return count, nil
}

func (runtime *Runtime) planningRepositoryByFullName(ctx context.Context, fullName string) (domain.Repository, error) {
	if runtime.planningDB == nil {
		return runtime.store.GetRepositoryByFullName(ctx, fullName)
	}
	var repository domain.Repository
	if err := runtime.planningDB.QueryRowContext(ctx, `
SELECT github_repo_id, full_name, monitoring_status, github_status
FROM repositories WHERE full_name = ? COLLATE NOCASE`, fullName).Scan(
		&repository.GitHubRepoID, &repository.FullName, &repository.MonitoringStatus, &repository.GitHubStatus,
	); err != nil {
		if err == sql.ErrNoRows {
			return domain.Repository{}, fmt.Errorf("repository %q was not found", fullName)
		}
		return domain.Repository{}, fmt.Errorf("read repository plan: %w", err)
	}
	return repository, nil
}

func (runtime *Runtime) planningTopicBySlug(ctx context.Context, slug string) (domain.Topic, error) {
	if runtime.planningDB == nil {
		return runtime.store.GetTopicBySlug(ctx, slug)
	}
	var topic domain.Topic
	if err := runtime.planningDB.QueryRowContext(ctx, `
SELECT id, slug, name, status FROM topics WHERE slug = ? COLLATE NOCASE`, slug).Scan(
		&topic.ID, &topic.Slug, &topic.Name, &topic.Status,
	); err != nil {
		if err == sql.ErrNoRows {
			return domain.Topic{}, fmt.Errorf("topic %q was not found", slug)
		}
		return domain.Topic{}, fmt.Errorf("read topic plan: %w", err)
	}
	return topic, nil
}

func (runtime *Runtime) planningProfileSuccess(ctx context.Context) (map[string]time.Time, error) {
	rows, err := runtime.planningDB.QueryContext(ctx, `
SELECT started_at, finished_at, details_json
FROM job_runs
WHERE job_type LIKE 'discover%' OR job_type = 'run-daily'
ORDER BY started_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("read prior discovery plan: %w", err)
	}
	defer rows.Close()
	result := make(map[string]time.Time)
	for rows.Next() {
		var startedRaw string
		var finishedRaw sql.NullString
		var detailsRaw string
		if err := rows.Scan(&startedRaw, &finishedRaw, &detailsRaw); err != nil {
			return nil, fmt.Errorf("scan prior discovery plan: %w", err)
		}
		when, err := time.Parse(time.RFC3339Nano, startedRaw)
		if err != nil {
			return nil, fmt.Errorf("parse prior discovery start: %w", err)
		}
		if finishedRaw.Valid {
			when, err = time.Parse(time.RFC3339Nano, finishedRaw.String)
			if err != nil {
				return nil, fmt.Errorf("parse prior discovery finish: %w", err)
			}
		}
		var details struct {
			Profiles  []SearchProfileReport `json:"profiles"`
			Discovery *struct {
				Profiles []SearchProfileReport `json:"profiles"`
			} `json:"discovery"`
		}
		if json.Unmarshal([]byte(detailsRaw), &details) != nil {
			continue
		}
		profiles := details.Profiles
		if details.Discovery != nil {
			profiles = append(profiles, details.Discovery.Profiles...)
		}
		for _, profile := range profiles {
			if profile.Skipped || profile.Error != "" || profile.IncompleteResults || profile.Truncated {
				continue
			}
			if previous, ok := result[profile.Name]; !ok || when.After(previous) {
				result[profile.Name] = when
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate prior discovery plan: %w", err)
	}
	return result, nil
}

func plannedAssignment(repository domain.Repository, topic domain.Topic, now time.Time) domain.TopicAssignmentResult {
	confidence := 1.0
	return domain.TopicAssignmentResult{
		Changed: true,
		Assignment: domain.RepositoryTopic{
			RepositoryID: repository.GitHubRepoID,
			TopicID:      topic.ID,
			Source:       domain.TopicSourceManual,
			Confirmed:    true,
			Confidence:   &confidence,
			AssignedAt:   now.UTC(),
			UpdatedAt:    now.UTC(),
		},
	}
}

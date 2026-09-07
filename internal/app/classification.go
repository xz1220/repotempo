package app

import (
	"context"
	"fmt"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/classification"
)

type ClassificationSuggestion struct {
	Repository string `json:"repository"`
	classification.Match
	Protected bool `json:"protected"`
	Existing  bool `json:"existing"`
}

type ClassificationReport struct {
	DryRun          bool                       `json:"dry_run"`
	RepositoryCount int                        `json:"repository_count"`
	MatchedCount    int                        `json:"matched_count"`
	ChangedCount    int                        `json:"changed_count"`
	ProtectedCount  int                        `json:"protected_count"`
	ByTopic         map[string]int             `json:"by_topic"`
	Suggestions     []ClassificationSuggestion `json:"suggestions"`
}

// ReclassifyTopics uses only saved metadata. Dry runs open the existing database
// read-only and do not create topics, write job records, or contact GitHub.
func (runtime *Runtime) ReclassifyTopics(ctx context.Context, dryRun bool) (ClassificationReport, error) {
	report := ClassificationReport{DryRun: dryRun, ByTopic: map[string]int{}, Suggestions: []ClassificationSuggestion{}}
	configured, err := config.LoadTopics(runtime.settings.TopicsConfig)
	if err != nil {
		return report, err
	}
	allowed := map[string]bool{}
	for _, topic := range configured.Flatten() {
		allowed[topic.Slug] = true
	}
	if !dryRun {
		if err := runtime.ensureTopics(ctx); err != nil {
			return report, err
		}
	}
	repositories, topics, assignments, err := runtime.classificationInputs(ctx)
	if err != nil {
		return report, err
	}
	report.RepositoryCount = len(repositories)
	topicSlugs := map[int64]string{}
	for _, topic := range topics {
		topicSlugs[topic.ID] = topic.Slug
	}
	evidence := map[int64][]string{}
	prior := map[string]domain.RepositoryTopic{}
	for _, assignment := range assignments {
		slug := topicSlugs[assignment.TopicID]
		prior[fmt.Sprintf("%d/%s", assignment.RepositoryID, slug)] = assignment
		if assignment.Source == domain.TopicSourceGitHub && (assignment.Confidence == nil || *assignment.Confidence > 0) {
			evidence[assignment.RepositoryID] = append(evidence[assignment.RepositoryID], slug)
		}
	}
	for _, repository := range repositories {
		matches := classification.Infer(repository.FullName, repository.Description, evidence[repository.GitHubRepoID])
		matched := false
		for _, match := range matches {
			if !allowed[match.Slug] {
				continue
			}
			matched = true
			report.ByTopic[match.Slug]++
			previous, exists := prior[fmt.Sprintf("%d/%s", repository.GitHubRepoID, match.Slug)]
			protected := exists && (previous.Source == domain.TopicSourceManual || previous.Confirmed)
			report.Suggestions = append(report.Suggestions, ClassificationSuggestion{Repository: repository.FullName, Match: match, Protected: protected, Existing: exists})
			if protected {
				report.ProtectedCount++
			}
			if dryRun && !protected && (!exists || previous.Source != domain.TopicSourceAuto || previous.Confidence == nil || *previous.Confidence != match.Confidence) {
				report.ChangedCount++
			}
		}
		if matched {
			report.MatchedCount++
		}
		if !dryRun {
			changed, err := classification.Apply(ctx, runtime.store, repository, evidence[repository.GitHubRepoID], runtime.now())
			report.ChangedCount += changed
			if err != nil {
				return report, err
			}
		}
	}
	return report, nil
}

func (runtime *Runtime) classificationInputs(ctx context.Context) ([]domain.Repository, []domain.Topic, []domain.RepositoryTopic, error) {
	if runtime.planningDB == nil {
		repositories, err := runtime.store.ListRepositories(ctx, domain.RepositoryFilter{})
		if err != nil {
			return nil, nil, nil, err
		}
		topics, err := runtime.store.ListTopics(ctx, "")
		if err != nil {
			return nil, nil, nil, err
		}
		assignments, err := runtime.store.ListRepositoryTopicAssignments(ctx, nil)
		return repositories, topics, assignments, err
	}
	rows, err := runtime.planningDB.QueryContext(ctx, "SELECT github_repo_id, full_name, description FROM repositories ORDER BY github_repo_id")
	if err != nil {
		return nil, nil, nil, err
	}
	repositories := []domain.Repository{}
	for rows.Next() {
		var repository domain.Repository
		if err := rows.Scan(&repository.GitHubRepoID, &repository.FullName, &repository.Description); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		repositories = append(repositories, repository)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = runtime.planningDB.QueryContext(ctx, "SELECT id, slug FROM topics")
	if err != nil {
		return nil, nil, nil, err
	}
	topics := []domain.Topic{}
	for rows.Next() {
		var topic domain.Topic
		if err := rows.Scan(&topic.ID, &topic.Slug); err != nil {
			rows.Close()
			return nil, nil, nil, err
		}
		topics = append(topics, topic)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, nil, nil, err
	}
	rows, err = runtime.planningDB.QueryContext(ctx, "SELECT repository_id, topic_id, source, confirmed, confidence FROM repository_topics")
	if err != nil {
		return nil, nil, nil, err
	}
	defer rows.Close()
	assignments := []domain.RepositoryTopic{}
	for rows.Next() {
		var assignment domain.RepositoryTopic
		if err := rows.Scan(&assignment.RepositoryID, &assignment.TopicID, &assignment.Source, &assignment.Confirmed, &assignment.Confidence); err != nil {
			return nil, nil, nil, err
		}
		assignments = append(assignments, assignment)
	}
	return repositories, topics, assignments, rows.Err()
}

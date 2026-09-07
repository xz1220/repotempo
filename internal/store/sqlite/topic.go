package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const topicColumns = `id, slug, name, parent_id, description, status, created_at, updated_at`

var topicSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

func scanTopic(row scanner) (domain.Topic, error) {
	var topic domain.Topic
	var parentID sql.NullInt64
	var createdAt string
	var updatedAt string
	if err := row.Scan(
		&topic.ID,
		&topic.Slug,
		&topic.Name,
		&parentID,
		&topic.Description,
		&topic.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		return domain.Topic{}, err
	}
	if parentID.Valid {
		value := parentID.Int64
		topic.ParentID = &value
	}
	var err error
	topic.CreatedAt, err = parseStoredTime(createdAt)
	if err != nil {
		return domain.Topic{}, err
	}
	topic.UpdatedAt, err = parseStoredTime(updatedAt)
	if err != nil {
		return domain.Topic{}, err
	}
	return topic, nil
}

func validateTopic(topic domain.Topic) error {
	if !topicSlugPattern.MatchString(topic.Slug) {
		return fmt.Errorf("%w: topic slug %q must contain lowercase letters, numbers, and single hyphens", corestore.ErrInvalid, topic.Slug)
	}
	if strings.TrimSpace(topic.Name) == "" {
		return fmt.Errorf("%w: topic name is required", corestore.ErrInvalid)
	}
	if topic.Status != "" && !topic.Status.Valid() {
		return fmt.Errorf("%w: invalid topic status %q", corestore.ErrInvalid, topic.Status)
	}
	if topic.ParentID != nil && *topic.ParentID <= 0 {
		return fmt.Errorf("%w: topic parent ID must be positive", corestore.ErrInvalid)
	}
	return nil
}

func (store *Store) UpsertTopic(ctx context.Context, topic domain.Topic) (domain.Topic, bool, error) {
	if err := validateTopic(topic); err != nil {
		return domain.Topic{}, false, err
	}
	now := store.nowUTC()
	if topic.Status == "" {
		topic.Status = domain.TopicActive
	}
	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Topic{}, false, fmt.Errorf("begin topic upsert: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()

	var existing domain.Topic
	if topic.ID > 0 {
		existing, err = scanTopic(transaction.QueryRowContext(ctx,
			"SELECT "+topicColumns+" FROM topics WHERE id = ?", topic.ID,
		))
	} else {
		existing, err = scanTopic(transaction.QueryRowContext(ctx,
			"SELECT "+topicColumns+" FROM topics WHERE slug = ? COLLATE NOCASE", topic.Slug,
		))
	}
	if errors.Is(err, sql.ErrNoRows) {
		topic.CreatedAt = now
		topic.UpdatedAt = now
		result, err := transaction.ExecContext(ctx, `
INSERT INTO topics (id, slug, name, parent_id, description, status, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			nullablePositiveID(topic.ID), topic.Slug, topic.Name, nullableInt64(topic.ParentID),
			topic.Description, topic.Status, storedTime(topic.CreatedAt), storedTime(topic.UpdatedAt),
		)
		if err != nil {
			return domain.Topic{}, false, fmt.Errorf("insert topic: %w", err)
		}
		if topic.ID == 0 {
			topic.ID, err = result.LastInsertId()
			if err != nil {
				return domain.Topic{}, false, fmt.Errorf("read inserted topic ID: %w", err)
			}
		}
		if err := transaction.Commit(); err != nil {
			return domain.Topic{}, false, fmt.Errorf("commit topic insert: %w", err)
		}
		return topic, true, nil
	}
	if err != nil {
		return domain.Topic{}, false, fmt.Errorf("read topic for upsert: %w", err)
	}
	if topic.ID > 0 && !strings.EqualFold(existing.Slug, topic.Slug) {
		var conflictingID int64
		err := transaction.QueryRowContext(ctx,
			"SELECT id FROM topics WHERE slug = ? COLLATE NOCASE AND id <> ? LIMIT 1",
			topic.Slug, topic.ID,
		).Scan(&conflictingID)
		if err == nil {
			return domain.Topic{}, false, fmt.Errorf("%w: topic slug %q belongs to ID %d", corestore.ErrInvalid, topic.Slug, conflictingID)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return domain.Topic{}, false, fmt.Errorf("check topic slug: %w", err)
		}
	}
	topic.ID = existing.ID
	topic.CreatedAt = existing.CreatedAt
	topic.UpdatedAt = now
	_, err = transaction.ExecContext(ctx, `
UPDATE topics
SET slug = ?, name = ?, parent_id = ?, description = ?, status = ?, updated_at = ?
WHERE id = ?`,
		topic.Slug, topic.Name, nullableInt64(topic.ParentID), topic.Description,
		topic.Status, storedTime(topic.UpdatedAt), topic.ID,
	)
	if err != nil {
		return domain.Topic{}, false, fmt.Errorf("update topic: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return domain.Topic{}, false, fmt.Errorf("commit topic update: %w", err)
	}
	return topic, false, nil
}

func nullablePositiveID(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func (store *Store) GetTopic(ctx context.Context, topicID int64) (domain.Topic, error) {
	topic, err := scanTopic(store.db.QueryRowContext(ctx,
		"SELECT "+topicColumns+" FROM topics WHERE id = ?", topicID,
	))
	if err != nil {
		return domain.Topic{}, mapNotFound(err)
	}
	return topic, nil
}

func (store *Store) GetTopicBySlug(ctx context.Context, slug string) (domain.Topic, error) {
	topic, err := scanTopic(store.db.QueryRowContext(ctx,
		"SELECT "+topicColumns+" FROM topics WHERE slug = ? COLLATE NOCASE", slug,
	))
	if err != nil {
		return domain.Topic{}, mapNotFound(err)
	}
	return topic, nil
}

func (store *Store) ListTopics(ctx context.Context, status domain.TopicStatus) ([]domain.Topic, error) {
	query := "SELECT " + topicColumns + " FROM topics"
	arguments := []any{}
	if status != "" {
		if !status.Valid() {
			return nil, fmt.Errorf("%w: invalid topic status %q", corestore.ErrInvalid, status)
		}
		query += " WHERE status = ?"
		arguments = append(arguments, status)
	}
	query += " ORDER BY CASE WHEN parent_id IS NULL THEN id ELSE parent_id END, parent_id IS NOT NULL, name COLLATE NOCASE"
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list topics: %w", err)
	}
	defer rows.Close()
	topics := make([]domain.Topic, 0)
	for rows.Next() {
		topic, err := scanTopic(rows)
		if err != nil {
			return nil, fmt.Errorf("scan topic: %w", err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate topics: %w", err)
	}
	return topics, nil
}

func validateAssignment(assignment domain.RepositoryTopic) error {
	if assignment.RepositoryID <= 0 || assignment.TopicID <= 0 {
		return fmt.Errorf("%w: repository and topic IDs must be positive", corestore.ErrInvalid)
	}
	if !assignment.Source.Valid() {
		return fmt.Errorf("%w: invalid topic source %q", corestore.ErrInvalid, assignment.Source)
	}
	if assignment.Source == domain.TopicSourceAuto && assignment.Confirmed {
		return fmt.Errorf("%w: automatic topic assignments cannot be confirmed", corestore.ErrInvalid)
	}
	if assignment.Confidence != nil && (*assignment.Confidence < 0 || *assignment.Confidence > 1) {
		return fmt.Errorf("%w: topic confidence must be between 0 and 1", corestore.ErrInvalid)
	}
	return nil
}

func scanAssignment(row scanner) (domain.RepositoryTopic, error) {
	var assignment domain.RepositoryTopic
	var confirmed int
	var confidence sql.NullFloat64
	var assignedAt string
	var updatedAt string
	if err := row.Scan(
		&assignment.RepositoryID,
		&assignment.TopicID,
		&assignment.Source,
		&confirmed,
		&confidence,
		&assignedAt,
		&updatedAt,
	); err != nil {
		return domain.RepositoryTopic{}, err
	}
	assignment.Confirmed = confirmed != 0
	if confidence.Valid {
		value := confidence.Float64
		assignment.Confidence = &value
	}
	var err error
	assignment.AssignedAt, err = parseStoredTime(assignedAt)
	if err != nil {
		return domain.RepositoryTopic{}, err
	}
	assignment.UpdatedAt, err = parseStoredTime(updatedAt)
	if err != nil {
		return domain.RepositoryTopic{}, err
	}
	return assignment, nil
}

func (store *Store) AssignRepositoryTopic(ctx context.Context, assignment domain.RepositoryTopic) (domain.TopicAssignmentResult, error) {
	if err := validateAssignment(assignment); err != nil {
		return domain.TopicAssignmentResult{}, err
	}
	now := store.nowUTC()
	if assignment.Source == domain.TopicSourceManual {
		assignment.Confirmed = true
	}
	if assignment.AssignedAt.IsZero() {
		assignment.AssignedAt = now
	} else {
		assignment.AssignedAt = assignment.AssignedAt.UTC()
	}
	assignment.UpdatedAt = now

	transaction, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.TopicAssignmentResult{}, fmt.Errorf("begin topic assignment: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	existing, err := scanAssignment(transaction.QueryRowContext(ctx, `
SELECT repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at
FROM repository_topics WHERE repository_id = ? AND topic_id = ?`,
		assignment.RepositoryID, assignment.TopicID,
	))
	if errors.Is(err, sql.ErrNoRows) {
		_, err = transaction.ExecContext(ctx, `
INSERT INTO repository_topics
    (repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			assignment.RepositoryID, assignment.TopicID, assignment.Source,
			boolInteger(assignment.Confirmed), nullableFloat64(assignment.Confidence),
			storedTime(assignment.AssignedAt), storedTime(assignment.UpdatedAt),
		)
		if err != nil {
			return domain.TopicAssignmentResult{}, fmt.Errorf("insert repository topic: %w", err)
		}
		if err := transaction.Commit(); err != nil {
			return domain.TopicAssignmentResult{}, fmt.Errorf("commit topic assignment: %w", err)
		}
		return domain.TopicAssignmentResult{Assignment: assignment, Changed: true}, nil
	}
	if err != nil {
		return domain.TopicAssignmentResult{}, fmt.Errorf("read repository topic: %w", err)
	}
	if (existing.Source == domain.TopicSourceManual || existing.Confirmed) && assignment.Source != domain.TopicSourceManual {
		if err := transaction.Commit(); err != nil {
			return domain.TopicAssignmentResult{}, fmt.Errorf("commit protected topic read: %w", err)
		}
		return domain.TopicAssignmentResult{Assignment: existing, Protected: true}, nil
	}
	if assignmentsEqual(existing, assignment) {
		if err := transaction.Commit(); err != nil {
			return domain.TopicAssignmentResult{}, fmt.Errorf("commit unchanged topic read: %w", err)
		}
		return domain.TopicAssignmentResult{Assignment: existing}, nil
	}
	_, err = transaction.ExecContext(ctx, `
UPDATE repository_topics
SET source = ?, confirmed = ?, confidence = ?, assigned_at = ?, updated_at = ?
WHERE repository_id = ? AND topic_id = ?`,
		assignment.Source, boolInteger(assignment.Confirmed), nullableFloat64(assignment.Confidence),
		storedTime(assignment.AssignedAt), storedTime(assignment.UpdatedAt),
		assignment.RepositoryID, assignment.TopicID,
	)
	if err != nil {
		return domain.TopicAssignmentResult{}, fmt.Errorf("update repository topic: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return domain.TopicAssignmentResult{}, fmt.Errorf("commit topic assignment update: %w", err)
	}
	return domain.TopicAssignmentResult{Assignment: assignment, Changed: true}, nil
}

func assignmentsEqual(left, right domain.RepositoryTopic) bool {
	if left.Source != right.Source || left.Confirmed != right.Confirmed {
		return false
	}
	if left.Confidence == nil || right.Confidence == nil {
		return left.Confidence == nil && right.Confidence == nil
	}
	return *left.Confidence == *right.Confidence
}

func (store *Store) RemoveRepositoryTopic(ctx context.Context, repositoryID, topicID int64, requestedBy domain.TopicSource) (bool, error) {
	if repositoryID <= 0 || topicID <= 0 || !requestedBy.Valid() {
		return false, fmt.Errorf("%w: valid repository, topic, and source are required", corestore.ErrInvalid)
	}
	if requestedBy == domain.TopicSourceManual {
		// A confirmed manual assignment with confidence 0 is a durable veto.
		// It uses the existing five-table model, stays visible in raw exports,
		// is excluded from topic queries, and protects against future auto adds.
		confidence := 0.0
		result, err := store.AssignRepositoryTopic(ctx, domain.RepositoryTopic{
			RepositoryID: repositoryID,
			TopicID:      topicID,
			Source:       domain.TopicSourceManual,
			Confirmed:    true,
			Confidence:   &confidence,
		})
		if err != nil {
			return false, err
		}
		return result.Changed, nil
	}
	query := "DELETE FROM repository_topics WHERE repository_id = ? AND topic_id = ?"
	arguments := []any{repositoryID, topicID}
	query += " AND source = ? AND source <> 'manual' AND confirmed = 0"
	arguments = append(arguments, requestedBy)
	result, err := store.db.ExecContext(ctx, query, arguments...)
	if err != nil {
		return false, fmt.Errorf("remove repository topic: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("read repository topic removal result: %w", err)
	}
	return affected > 0, nil
}

func (store *Store) ListRepositoryTopics(ctx context.Context, repositoryID int64) ([]domain.Topic, error) {
	rows, err := store.db.QueryContext(ctx, `
SELECT t.`+strings.ReplaceAll(topicColumns, ", ", ", t.")+`
FROM topics t
JOIN repository_topics rt ON rt.topic_id = t.id
WHERE rt.repository_id = ?
  AND NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
ORDER BY t.name COLLATE NOCASE`, repositoryID)
	if err != nil {
		return nil, fmt.Errorf("list repository topics: %w", err)
	}
	defer rows.Close()
	topics := make([]domain.Topic, 0)
	for rows.Next() {
		topic, err := scanTopic(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository topic: %w", err)
		}
		topics = append(topics, topic)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository topics: %w", err)
	}
	return topics, nil
}

func (store *Store) ListRepositoryTopicAssignments(ctx context.Context, repositoryID *int64) ([]domain.RepositoryTopic, error) {
	query := `
SELECT repository_id, topic_id, source, confirmed, confidence, assigned_at, updated_at
FROM repository_topics`
	arguments := []any{}
	if repositoryID != nil {
		query += " WHERE repository_id = ?"
		arguments = append(arguments, *repositoryID)
	}
	query += " ORDER BY repository_id, topic_id"
	rows, err := store.db.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("list repository topic assignments: %w", err)
	}
	defer rows.Close()
	assignments := make([]domain.RepositoryTopic, 0)
	for rows.Next() {
		assignment, err := scanAssignment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan repository topic assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository topic assignments: %w", err)
	}
	return assignments, nil
}

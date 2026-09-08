package sqlite

import (
	"context"
	"fmt"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

// ListRepositoryTags intentionally uses the entire registry as of the selected
// date. A daily-additions, watchlist, or search filter must not hide available
// labels from the selector. One repository counts once per label even when the
// same label came from GitHub, research, and an effective taxonomy assignment.
func (store *Store) ListRepositoryTags(ctx context.Context, asOf domain.Date) ([]domain.RepositoryTag, error) {
	if asOf == "" {
		asOf = domain.ShanghaiDate(store.nowUTC())
	}
	if err := asOf.Validate(); err != nil {
		return nil, fmt.Errorf("%w: invalid tag observation date: %v", corestore.ErrInvalid, err)
	}
	rows, err := store.db.QueryContext(ctx, `
WITH entered AS (
    SELECT github_repo_id, github_topics_json, research_tags_json
    FROM repositories WHERE date(first_seen_at, '+8 hours') <= ?
), all_labels AS (
    SELECT r.github_repo_id AS repository_id, label.value AS name
    FROM entered r, json_each(COALESCE(r.github_topics_json, '[]')) label WHERE label.type = 'text'
    UNION
    SELECT r.github_repo_id, label.value
    FROM entered r, json_each(r.research_tags_json) label WHERE label.type = 'text'
    UNION
    SELECT rt.repository_id, lower(trim(t.slug))
    FROM entered r JOIN repository_topics rt ON rt.repository_id = r.github_repo_id
    JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
    WHERE NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
    UNION
    SELECT rt.repository_id, lower(trim(t.name))
    FROM entered r JOIN repository_topics rt ON rt.repository_id = r.github_repo_id
    JOIN topics t ON t.id = rt.topic_id AND t.status = 'active'
    WHERE NOT (rt.source = 'manual' AND rt.confirmed = 1 AND COALESCE(rt.confidence, -1) = 0)
)
SELECT name, COUNT(DISTINCT repository_id)
FROM all_labels WHERE name <> ''
GROUP BY name ORDER BY COUNT(DISTINCT repository_id) DESC, name ASC`, asOf)
	if err != nil {
		return nil, fmt.Errorf("query repository tag options: %w", err)
	}
	defer rows.Close()
	tags := make([]domain.RepositoryTag, 0)
	for rows.Next() {
		var tag domain.RepositoryTag
		if err := rows.Scan(&tag.Name, &tag.Count); err != nil {
			return nil, fmt.Errorf("scan repository tag option: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository tag options: %w", err)
	}
	return tags, nil
}

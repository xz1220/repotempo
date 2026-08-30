package app

import (
	"context"
	"fmt"

	"github.com/xz1220/github-radar/internal/config"
	"github.com/xz1220/github-radar/internal/domain"
)

// ensureTopics materializes the configured two-level taxonomy in parent-first
// order. UpsertTopic makes repeated command runs safe and keeps the config file
// as the source of truth without adding another table.
func (runtime *Runtime) ensureTopics(ctx context.Context) error {
	configured, err := config.LoadTopics(runtime.settings.TopicsConfig)
	if err != nil {
		return err
	}
	ids := make(map[string]int64)
	for _, definition := range configured.Flatten() {
		var parentID *int64
		if definition.ParentSlug != "" {
			value, ok := ids[definition.ParentSlug]
			if !ok {
				return fmt.Errorf("topic %q references parent %q before it is defined", definition.Slug, definition.ParentSlug)
			}
			parentID = &value
		}
		topic, _, err := runtime.store.UpsertTopic(ctx, domain.Topic{
			Slug:        definition.Slug,
			Name:        definition.Name,
			ParentID:    parentID,
			Description: definition.Description,
			Status:      domain.TopicStatus(definition.Status),
		})
		if err != nil {
			return fmt.Errorf("seed topic %q: %w", definition.Slug, err)
		}
		ids[definition.Slug] = topic.ID
	}
	return nil
}

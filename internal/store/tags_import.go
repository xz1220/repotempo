package store

import (
	"context"
	"github.com/xz1220/repotempo/internal/domain"
)

type TagsImportStore interface {
	ImportRepositoryTags(context.Context, []domain.RepositoryTagsImport) (domain.TagsBatchResult, error)
}

package app

import (
	"context"
	"errors"

	corestore "github.com/xz1220/repotempo/internal/store"
	"github.com/xz1220/repotempo/internal/web"
)

var _ web.FocusUpdater = (*Runtime)(nil)

// SetRepositoryFocus exposes only the explicit focus flag to the Web layer.
// Store failures are reduced to stable categories before crossing that boundary.
func (runtime *Runtime) SetRepositoryFocus(ctx context.Context, repositoryID int64, focus bool) error {
	err := runtime.store.SetRepositoryFocus(ctx, repositoryID, focus)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, corestore.ErrInvalid):
		return web.ErrFocusInvalid
	case errors.Is(err, corestore.ErrNotFound):
		return web.ErrFocusNotFound
	default:
		return web.ErrFocusUnavailable
	}
}

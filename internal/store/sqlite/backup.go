package sqlite

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	corestore "github.com/xz1220/repotempo/internal/store"
)

var _ corestore.Backuper = (*Store)(nil)

// Backup creates a transactionally consistent SQLite copy with VACUUM INTO.
// It refuses to overwrite an existing path so an export can never destroy a
// previous artifact silently.
func (store *Store) Backup(ctx context.Context, destination string) error {
	if strings.TrimSpace(destination) == "" {
		return fmt.Errorf("%w: backup destination is required", corestore.ErrInvalid)
	}
	if store.path == ":memory:" {
		return fmt.Errorf("%w: in-memory databases cannot be exported with this backup method", corestore.ErrInvalid)
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return fmt.Errorf("resolve backup destination: %w", err)
	}
	if sourcePath, sourceErr := filepath.Abs(store.path); sourceErr == nil && sourcePath == destinationPath {
		return fmt.Errorf("%w: backup destination must differ from the source database", corestore.ErrInvalid)
	}
	if _, err := os.Stat(destinationPath); err == nil {
		return fmt.Errorf("%w: backup destination already exists", corestore.ErrInvalid)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect backup destination: %w", err)
	}
	if _, err := store.db.ExecContext(ctx, "VACUUM INTO ?", destinationPath); err != nil {
		return fmt.Errorf("create SQLite backup: %w", err)
	}
	return nil
}

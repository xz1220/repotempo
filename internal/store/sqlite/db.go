package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	corestore "github.com/xz1220/github-radar/internal/store"
	"github.com/xz1220/github-radar/migrations"
	_ "modernc.org/sqlite"
)

const defaultBusyTimeout = 5 * time.Second

type Config struct {
	Path        string
	BusyTimeout time.Duration
	Now         func() time.Time
}

type Store struct {
	db   *sql.DB
	now  func() time.Time
	path string
}

var _ corestore.Store = (*Store)(nil)

func Open(ctx context.Context, path string) (*Store, error) {
	return OpenWithConfig(ctx, Config{Path: path})
}

func OpenWithConfig(ctx context.Context, config Config) (*Store, error) {
	if strings.TrimSpace(config.Path) == "" {
		return nil, fmt.Errorf("%w: SQLite path is required", corestore.ErrInvalid)
	}
	if config.BusyTimeout <= 0 {
		config.BusyTimeout = defaultBusyTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}

	database, err := sql.Open("sqlite", config.Path)
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// Foreign-key PRAGMAs are connection-local. A single pooled connection also
	// matches the application's one-writer design; WAL still permits other
	// processes (the collector and read-only Web service) to coexist.
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	closeOnError := func(cause error) (*Store, error) {
		_ = database.Close()
		return nil, cause
	}
	if err := database.PingContext(ctx); err != nil {
		return closeOnError(fmt.Errorf("ping SQLite database: %w", err))
	}
	if _, err := database.ExecContext(ctx, "PRAGMA foreign_keys = ON"); err != nil {
		return closeOnError(fmt.Errorf("enable SQLite foreign keys: %w", err))
	}
	busyMilliseconds := config.BusyTimeout.Milliseconds()
	if busyMilliseconds > 2_147_483_647 {
		busyMilliseconds = 2_147_483_647
	}
	if _, err := database.ExecContext(ctx, fmt.Sprintf("PRAGMA busy_timeout = %d", busyMilliseconds)); err != nil {
		return closeOnError(fmt.Errorf("set SQLite busy timeout: %w", err))
	}
	var journalMode string
	if err := database.QueryRowContext(ctx, "PRAGMA journal_mode = WAL").Scan(&journalMode); err != nil {
		return closeOnError(fmt.Errorf("enable SQLite WAL: %w", err))
	}
	if config.Path != ":memory:" && !strings.EqualFold(journalMode, "wal") {
		return closeOnError(fmt.Errorf("enable SQLite WAL: database returned journal mode %q", journalMode))
	}
	if _, err := database.ExecContext(ctx, "PRAGMA synchronous = NORMAL"); err != nil {
		return closeOnError(fmt.Errorf("set SQLite synchronous mode: %w", err))
	}
	if err := applyMigrations(ctx, database); err != nil {
		return closeOnError(err)
	}

	return &Store{db: database, now: config.Now, path: config.Path}, nil
}

func applyMigrations(ctx context.Context, database *sql.DB) error {
	entries, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return fmt.Errorf("list embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migrations: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	for _, name := range names {
		script, err := fs.ReadFile(migrations.Files, name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if _, err := transaction.ExecContext(ctx, string(script)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit migrations: %w", err)
	}
	return nil
}

func (store *Store) Close() error {
	if store == nil || store.db == nil {
		return nil
	}
	return store.db.Close()
}

func (store *Store) nowUTC() time.Time {
	return store.now().UTC()
}

func storedTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseStoredTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}

func parseNullableStoredTime(value sql.NullString) (*time.Time, error) {
	if !value.Valid {
		return nil, nil
	}
	parsed, err := parseStoredTime(value.String)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

func boolInteger(value bool) int {
	if value {
		return 1
	}
	return 0
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return corestore.ErrNotFound
	}
	return err
}

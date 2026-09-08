package sqlite

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	modernsqlite "modernc.org/sqlite"
)

// configuredConnector applies connection-local policy before database/sql can
// use a physical connection. A single-connection pool can still replace that
// connection after cancellation or driver.ErrBadConn.
type configuredConnector struct {
	driver.Connector
	busyMilliseconds int64
}

func (connector configuredConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connection, err := connector.Connector.Connect(ctx)
	if err != nil {
		return nil, err
	}
	executor, ok := connection.(driver.ExecerContext)
	if !ok {
		_ = connection.Close()
		return nil, fmt.Errorf("SQLite driver does not support connection initialization")
	}
	for _, statement := range []string{
		fmt.Sprintf("PRAGMA busy_timeout = %d", connector.busyMilliseconds),
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := executor.ExecContext(ctx, statement, nil); err != nil {
			_ = connection.Close()
			return nil, fmt.Errorf("configure SQLite connection (%s): %w", statement, err)
		}
	}
	return connection, nil
}

func openConfiguredSQLite(path string, timeout time.Duration) (*sql.DB, error) {
	dsn := path
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve SQLite path: %w", err)
		}
		// A filename's ?, # and % are literal path characters, not DSN query
		// delimiters. Existing file: URIs already encode their path and options.
		dsn = (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute)}).String()
	}
	base, err := modernsqlite.NewConnector(dsn)
	if err != nil {
		return nil, err
	}
	if timeout <= 0 {
		timeout = defaultBusyTimeout
	}
	milliseconds := timeout.Milliseconds()
	if milliseconds > 2_147_483_647 {
		milliseconds = 2_147_483_647
	}
	return sql.OpenDB(configuredConnector{Connector: base, busyMilliseconds: milliseconds}), nil
}

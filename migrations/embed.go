// Package migrations exposes the idempotent SQLite schema bundled into the
// github-radar binary.
package migrations

import "embed"

// Files contains the SQL migrations run through the store migration runner.
// SQLite user_version makes repeated startup idempotent; table rebuilds also
// require the runner's connection settings and integrity checks.
//
//go:embed *.sql
var Files embed.FS

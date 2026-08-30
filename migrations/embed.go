// Package migrations exposes the idempotent SQLite schema bundled into the
// github-radar binary.
package migrations

import "embed"

// Files contains all SQL migration files. Migrations intentionally use only
// idempotent statements; schema state is tracked with SQLite user_version so no
// sixth application table is needed.
//
//go:embed *.sql
var Files embed.FS

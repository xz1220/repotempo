package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// applyRepositoryRebuildMigration follows SQLite's generalized ALTER TABLE
// procedure. Foreign-key enforcement must be disabled BEFORE the transaction,
// otherwise dropping repositories can cascade-delete its child data. Keeping
// one connection also prevents leaking the temporary PRAGMAs to other work.
func applyRepositoryRebuildMigration(ctx context.Context, database *sql.DB, version int, script string) (returnErr error) {
	connection, err := database.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	var foreignKeys, legacyAlter int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		return err
	}
	if err := connection.QueryRowContext(ctx, "PRAGMA legacy_alter_table").Scan(&legacyAlter); err != nil {
		return err
	}
	defer func() {
		// Request cancellation must not skip cleanup. A failed restoration makes
		// Open fail, so no application can continue using a weakened connection.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, setting := range []struct {
			name  string
			value int
		}{{"legacy_alter_table", legacyAlter}, {"foreign_keys", foreignKeys}} {
			if _, err := connection.ExecContext(cleanup, fmt.Sprintf("PRAGMA %s = %d", setting.name, setting.value)); err != nil {
				returnErr = errors.Join(returnErr, fmt.Errorf("restore %s after migration: %w", setting.name, err))
				continue
			}
			var actual int
			if err := connection.QueryRowContext(cleanup, "PRAGMA "+setting.name).Scan(&actual); err != nil || actual != setting.value {
				returnErr = errors.Join(returnErr, fmt.Errorf("verify %s restoration (got %d, want %d): %v", setting.name, actual, setting.value, err))
			}
		}
	}()
	if _, err := connection.ExecContext(ctx, "PRAGMA foreign_keys = OFF"); err != nil {
		return err
	}
	// Avoid rewriting unrelated trigger/view references while the replacement
	// takes the old table's name. Existing views remain intact and valid.
	if _, err := connection.ExecContext(ctx, "PRAGMA legacy_alter_table = ON"); err != nil {
		return err
	}
	var disabled int
	if err := connection.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&disabled); err != nil || disabled != 0 {
		return fmt.Errorf("cannot safely disable foreign keys for repository rebuild: %v", err)
	}
	// Own rollback synchronously. If BeginTx inherits request cancellation,
	// database/sql's background rollback may mark the Tx done before the
	// physical ROLLBACK finishes. Cleanup could then restore foreign_keys while
	// still inside that transaction, where SQLite silently ignores the change.
	// Individual SQL operations remain cancellable through the original ctx.
	transaction, err := connection.BeginTx(context.WithoutCancel(ctx), nil)
	if err != nil {
		return err
	}
	defer func() {
		if err := transaction.Rollback(); err != nil && !errors.Is(err, sql.ErrTxDone) {
			returnErr = errors.Join(returnErr, fmt.Errorf("rollback repository migration: %w", err))
		}
	}()
	var current int
	if err := transaction.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	if current >= version {
		if err := ctx.Err(); err != nil {
			return err
		}
		return transaction.Commit()
	}
	if err := validateRepositoryRebuildColumns(ctx, transaction); err != nil {
		return err
	}
	objects, err := repositorySchemaObjects(ctx, transaction)
	if err != nil {
		return err
	}
	before, err := foreignKeyViolations(ctx, transaction)
	if err != nil {
		return err
	}
	if _, err := transaction.ExecContext(ctx, script); err != nil {
		return err
	}
	for _, object := range objects {
		if _, err := transaction.ExecContext(ctx, object); err != nil {
			return fmt.Errorf("restore repository index or trigger: %w", err)
		}
	}
	after, err := foreignKeyViolations(ctx, transaction)
	if err != nil {
		return err
	}
	if !slices.Equal(before, after) {
		return fmt.Errorf("repository rebuild changed existing foreign-key violations (before %d, after %d)", len(before), len(after))
	}
	if _, err := transaction.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return transaction.Commit()
}

func validateRepositoryRebuildColumns(ctx context.Context, transaction *sql.Tx) error {
	rows, err := transaction.QueryContext(ctx, "SELECT name FROM pragma_table_xinfo('repositories') ORDER BY cid")
	if err != nil {
		return err
	}
	defer rows.Close()
	var actual []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		actual = append(actual, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	expected := strings.Split(repositoryColumnsV4, ",")
	for index := range expected {
		expected[index] = strings.TrimSpace(expected[index])
	}
	if !slices.Equal(actual, expected) {
		return errors.New("repository columns differ from the supported v3 schema; refusing to discard or reinterpret custom metadata")
	}
	return nil
}

func repositorySchemaObjects(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, `SELECT sql FROM sqlite_schema
		WHERE tbl_name = 'repositories' AND type IN ('index', 'trigger') AND sql IS NOT NULL
		ORDER BY type, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var definitions []string
	for rows.Next() {
		var definition string
		if err := rows.Scan(&definition); err != nil {
			return nil, err
		}
		definitions = append(definitions, definition)
	}
	return definitions, rows.Err()
}

func foreignKeyViolations(ctx context.Context, transaction *sql.Tx) ([]string, error) {
	rows, err := transaction.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var violations []string
	for rows.Next() {
		var table, parent string
		var row sql.NullInt64
		var constraint int
		if err := rows.Scan(&table, &row, &parent, &constraint); err != nil {
			return nil, err
		}
		violations = append(violations, fmt.Sprintf("%q|%v|%q|%d", table, row, parent, constraint))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Sort(violations)
	return violations, nil
}

package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

// AdoptLegacyWorkspace is called with the first explicitly configured admin,
// before public login is served. It is idempotent and never selects an owner
// based on who signs in first. The source is kept for operator CLI compatibility.
func (store *Store) AdoptLegacyWorkspace(ctx context.Context, ownerID int64) error {
	if ownerID <= 0 {
		return corestore.ErrInvalid
	}
	return store.authTransaction(ctx, func(conn *sql.Conn) error {
		now := authTime(store.nowUTC())
		if _, err := conn.ExecContext(ctx, `INSERT INTO users(github_user_id,login,created_at,updated_at) VALUES (?,'',?,?) ON CONFLICT DO NOTHING`, ownerID, now, now); err != nil {
			return err
		}
		var exists bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_migrations WHERE name='legacy-workspace')`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO user_repositories(user_id,repository_id,is_focus,note,created_at,updated_at)
SELECT ?,github_repo_id,is_focus,substr(manual_note,1,2000),?,? FROM repositories WHERE is_focus=1 OR manual_note<>''
ON CONFLICT(user_id,repository_id) DO NOTHING`, ownerID, now, now); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `INSERT INTO account_migrations(name,owner_id,created_at) VALUES ('legacy-workspace',?,?)`, ownerID, now)
		return err
	})
}

func (store *Store) UserRepositoryStates(ctx context.Context, userID int64, ids []int64) (map[int64]domain.UserRepositoryState, error) {
	result := make(map[int64]domain.UserRepositoryState)
	if principal, scoped := domain.PrincipalFromContext(ctx); scoped && principal.UserID != userID {
		return nil, corestore.ErrInvalid
	}
	if userID <= 0 || len(ids) == 0 {
		return result, nil
	}
	for _, chunk := range chunks(ids, 500) {
		args := append([]any{userID}, anyIDs(chunk)...)
		rows, err := store.db.QueryContext(ctx, `SELECT repository_id,is_focus,note FROM user_repositories WHERE user_id=? AND repository_id IN (`+placeholders(len(chunk))+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var state domain.UserRepositoryState
			if err := rows.Scan(&state.RepositoryID, &state.IsFocus, &state.Note); err != nil {
				rows.Close()
				return nil, err
			}
			result[state.RepositoryID] = state
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (store *Store) SetUserRepositoryFocus(ctx context.Context, userID, repositoryID int64, focus bool) error {
	return store.setUserRepository(ctx, userID, repositoryID, &focus, nil)
}
func (store *Store) SetUserRepositoryNote(ctx context.Context, userID, repositoryID int64, note string) error {
	return store.setUserRepository(ctx, userID, repositoryID, nil, &note)
}
func (store *Store) SetUserRepositoryState(ctx context.Context, userID, repositoryID int64, focus bool, note string) error {
	return store.setUserRepository(ctx, userID, repositoryID, &focus, &note)
}

func (store *Store) setUserRepository(ctx context.Context, userID, repositoryID int64, focus *bool, note *string) error {
	if userID <= 0 || repositoryID <= 0 || note != nil && (!utf8.ValidString(*note) || utf8.RuneCountInString(*note) > 2000) {
		return corestore.ErrInvalid
	}
	if principal, ok := domain.PrincipalFromContext(ctx); ok && principal.UserID != userID {
		return corestore.ErrInvalid
	}
	return store.authTransaction(ctx, func(conn *sql.Conn) error {
		var exists bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM repositories WHERE github_repo_id=?) AND EXISTS(SELECT 1 FROM users WHERE github_user_id=?)`, repositoryID, userID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return corestore.ErrNotFound
		}
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_repositories WHERE user_id=? AND repository_id=?)`, userID, repositoryID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			var count int
			if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_repositories WHERE user_id=?`, userID).Scan(&count); err != nil {
				return err
			}
			if count >= domain.UserRepositoryLimit {
				return domain.ErrUserRepositoryLimit
			}
		}
		now := authTime(store.nowUTC())
		if _, err := conn.ExecContext(ctx, `INSERT INTO user_repositories(user_id,repository_id,created_at,updated_at) VALUES (?,?,?,?) ON CONFLICT DO NOTHING`, userID, repositoryID, now, now); err != nil {
			return err
		}
		if focus != nil {
			if _, err := conn.ExecContext(ctx, `UPDATE user_repositories SET is_focus=?,updated_at=? WHERE user_id=? AND repository_id=?`, *focus, now, userID, repositoryID); err != nil {
				return err
			}
		}
		if note != nil {
			if _, err := conn.ExecContext(ctx, `UPDATE user_repositories SET note=?,updated_at=? WHERE user_id=? AND repository_id=?`, strings.TrimSpace(*note), now, userID, repositoryID); err != nil {
				return err
			}
		}
		_, err := conn.ExecContext(ctx, `DELETE FROM user_repositories WHERE user_id=? AND repository_id=? AND is_focus=0 AND note=''`, userID, repositoryID)
		return err
	})
}

// Queries from public or authenticated request contexts never use the shared
// legacy focus flag. Internal collection jobs without a principal retain it.
func personalFocusPredicate(ctx context.Context, alias string) (string, []any) {
	principal, scoped := domain.PrincipalFromContext(ctx)
	if !scoped {
		return alias + ".is_focus = 1", nil
	}
	if principal.UserID <= 0 {
		return "0 = 1", nil
	}
	return `EXISTS (SELECT 1 FROM user_repositories personal WHERE personal.user_id=? AND personal.repository_id=` + alias + `.github_repo_id AND personal.is_focus=1)`, []any{principal.UserID}
}

func trendScopeForContext(ctx context.Context, query domain.RepositoryTrendQuery) (string, []any) {
	focus := query.OnlyFocus
	query.OnlyFocus = false
	statement, args := trendScope(query)
	if focus {
		condition, personalArgs := personalFocusPredicate(ctx, "r")
		statement += " AND " + condition
		args = append(args, personalArgs...)
	}
	return statement, args
}

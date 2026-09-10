package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

const authStateCapacity = 512
const authSessionsPerUser = 20

var authVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var authLoginPattern = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

func validAuthHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validAuthTime(value time.Time) bool {
	year := value.UTC().Year()
	return !value.IsZero() && year >= 1 && year <= 9999
}

func validAuthLifetime(created, expires, now time.Time, maximum time.Duration) bool {
	return validAuthTime(created) && validAuthTime(expires) && validAuthTime(now) && !created.After(now) &&
		expires.After(now) && expires.After(created) && expires.Sub(created) <= maximum
}

// Fixed-width UTC timestamps preserve exact expiration and FIFO comparisons;
// variable RFC3339Nano strings and SQLite julianday both lose that ordering.
func authTime(value time.Time) string { return value.UTC().Format("2006-01-02T15:04:05.000000000Z") }

func validAuthReturnPath(value string) bool {
	if value == "" || len(value) > 8192 || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") ||
		strings.HasPrefix(value, "//") || strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl) {
		return false
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" ||
		strings.HasPrefix(parsed.Path, "//") || strings.Contains(parsed.Path, "\\") || strings.ContainsFunc(parsed.Path, unicode.IsControl) {
		return false
	}
	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return false
	}
	for key, values := range query {
		if strings.ContainsFunc(key, unicode.IsControl) {
			return false
		}
		for _, value := range values {
			if strings.ContainsFunc(value, unicode.IsControl) {
				return false
			}
		}
	}
	return !strings.ContainsFunc(parsed.Fragment, unicode.IsControl)
}

// Use the configured physical connection and an immediate transaction so
// cleanup/capacity checks/insert and state consumption serialize across processes.
func (store *Store) authTransaction(ctx context.Context, operation func(*sql.Conn) error) error {
	connection, err := store.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer connection.Close()
	if _, err := connection.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = connection.ExecContext(cleanup, "ROLLBACK")
	}()
	if err := operation(connection); err != nil {
		return err
	}
	_, err = connection.ExecContext(ctx, "COMMIT")
	return err
}

func (store *Store) PutOAuthLoginState(ctx context.Context, state domain.OAuthLoginState) error {
	now := store.nowUTC()
	if !validAuthHash(state.StateHash) || !validAuthHash(state.BindingHash) || !authVerifierPattern.MatchString(state.Verifier) ||
		!validAuthReturnPath(state.ReturnPath) || !validAuthLifetime(state.CreatedAt, state.ExpiresAt, now, 10*time.Minute) {
		return fmt.Errorf("%w: invalid OAuth login state", corestore.ErrInvalid)
	}
	state.StateHash, state.BindingHash = strings.ToLower(state.StateHash), strings.ToLower(state.BindingHash)
	return store.authTransaction(ctx, func(connection *sql.Conn) error {
		if _, err := connection.ExecContext(ctx, "DELETE FROM oauth_login_states WHERE expires_at <= ?", authTime(now)); err != nil {
			return err
		}
		var count int
		if err := connection.QueryRowContext(ctx, "SELECT COUNT(*) FROM oauth_login_states").Scan(&count); err != nil {
			return err
		}
		if count >= authStateCapacity {
			return domain.ErrAuthCapacity
		}
		var exists bool
		if err := connection.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM oauth_login_states WHERE state_hash=?)", state.StateHash).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: OAuth state already exists", corestore.ErrInvalid)
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO oauth_login_states(state_hash,binding_hash,verifier,return_path,created_at,expires_at) VALUES (?,?,?,?,?,?)`,
			state.StateHash, state.BindingHash, state.Verifier, state.ReturnPath, authTime(state.CreatedAt), authTime(state.ExpiresAt))
		return err
	})
}

func (store *Store) ConsumeOAuthLoginState(ctx context.Context, stateHash, bindingHash string, now time.Time) (domain.OAuthLoginState, error) {
	if !validAuthHash(stateHash) || !validAuthHash(bindingHash) || !validAuthTime(now) {
		return domain.OAuthLoginState{}, fmt.Errorf("%w: invalid OAuth state lookup", corestore.ErrInvalid)
	}
	var state domain.OAuthLoginState
	err := store.authTransaction(ctx, func(connection *sql.Conn) error {
		var created, expires string
		err := connection.QueryRowContext(ctx, `DELETE FROM oauth_login_states
WHERE state_hash=? AND binding_hash=? AND created_at<=? AND expires_at>?
RETURNING state_hash,binding_hash,verifier,return_path,created_at,expires_at`,
			strings.ToLower(stateHash), strings.ToLower(bindingHash), authTime(now), authTime(now)).Scan(&state.StateHash, &state.BindingHash, &state.Verifier, &state.ReturnPath, &created, &expires)
		if err != nil {
			return mapNotFound(err)
		}
		state.CreatedAt, err = parseStoredTime(created)
		if err != nil {
			return err
		}
		state.ExpiresAt, err = parseStoredTime(expires)
		if err != nil {
			return err
		}
		if !authVerifierPattern.MatchString(state.Verifier) || !validAuthReturnPath(state.ReturnPath) || !validAuthLifetime(state.CreatedAt, state.ExpiresAt, now, 10*time.Minute) {
			return fmt.Errorf("%w: invalid stored OAuth state", corestore.ErrInvalid)
		}
		return nil
	})
	if err != nil {
		return domain.OAuthLoginState{}, err
	}
	return state, nil
}

func (store *Store) PutAuthSession(ctx context.Context, tokenHash string, session domain.AuthSession) error {
	now := store.nowUTC()
	if !validAuthHash(tokenHash) || !validAuthHash(session.CSRFToken) || session.GitHubUserID <= 0 || !authLoginPattern.MatchString(session.Login) || strings.Contains(session.Login, "--") ||
		!validAuthLifetime(session.CreatedAt, session.ExpiresAt, now, 24*time.Hour) {
		return fmt.Errorf("%w: invalid auth session", corestore.ErrInvalid)
	}
	tokenHash, session.CSRFToken = strings.ToLower(tokenHash), strings.ToLower(session.CSRFToken)
	return store.authTransaction(ctx, func(connection *sql.Conn) error {
		if _, err := connection.ExecContext(ctx, "DELETE FROM web_auth_sessions WHERE expires_at<=?", authTime(now)); err != nil {
			return err
		}
		var exists bool
		if err := connection.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM web_auth_sessions WHERE token_hash=?)", tokenHash).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("%w: auth session already exists", corestore.ErrInvalid)
		}
		// Keep the newest 19 existing sessions and the newly authenticated one.
		// This affects only this GitHub identity; another user's session survives.
		if _, err := connection.ExecContext(ctx, `DELETE FROM web_auth_sessions WHERE token_hash IN (
SELECT token_hash FROM web_auth_sessions WHERE github_user_id=? ORDER BY created_at DESC,rowid DESC LIMIT -1 OFFSET ?)`, session.GitHubUserID, authSessionsPerUser-1); err != nil {
			return err
		}
		_, err := connection.ExecContext(ctx, `INSERT INTO web_auth_sessions(token_hash,github_user_id,login,csrf_token,created_at,expires_at) VALUES (?,?,?,?,?,?)`,
			tokenHash, session.GitHubUserID, session.Login, session.CSRFToken, authTime(session.CreatedAt), authTime(session.ExpiresAt))
		return err
	})
}

func (store *Store) GetAuthSession(ctx context.Context, tokenHash string, now time.Time) (domain.AuthSession, error) {
	if !validAuthHash(tokenHash) || !validAuthTime(now) {
		return domain.AuthSession{}, fmt.Errorf("%w: invalid auth session lookup", corestore.ErrInvalid)
	}
	var session domain.AuthSession
	var created, expires string
	err := store.db.QueryRowContext(ctx, `SELECT github_user_id,login,csrf_token,created_at,expires_at FROM web_auth_sessions
WHERE token_hash=? AND created_at<=? AND expires_at>?`, strings.ToLower(tokenHash), authTime(now), authTime(now)).Scan(&session.GitHubUserID, &session.Login, &session.CSRFToken, &created, &expires)
	if err != nil {
		return domain.AuthSession{}, mapNotFound(err)
	}
	session.CreatedAt, err = parseStoredTime(created)
	if err != nil {
		return domain.AuthSession{}, err
	}
	session.ExpiresAt, err = parseStoredTime(expires)
	if err != nil {
		return domain.AuthSession{}, err
	}
	if session.GitHubUserID <= 0 || !authLoginPattern.MatchString(session.Login) || strings.Contains(session.Login, "--") || !validAuthHash(session.CSRFToken) || !validAuthLifetime(session.CreatedAt, session.ExpiresAt, now, 24*time.Hour) {
		return domain.AuthSession{}, fmt.Errorf("%w: invalid stored auth session", corestore.ErrInvalid)
	}
	return session, nil
}

func (store *Store) DeleteAuthSession(ctx context.Context, tokenHash string) error {
	if !validAuthHash(tokenHash) {
		return fmt.Errorf("%w: invalid auth session hash", corestore.ErrInvalid)
	}
	_, err := store.db.ExecContext(ctx, "DELETE FROM web_auth_sessions WHERE token_hash=?", strings.ToLower(tokenHash))
	return err
}

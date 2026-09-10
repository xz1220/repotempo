package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	corestore "github.com/xz1220/repotempo/internal/store"
)

var agentIDPattern = regexp.MustCompile(`^rt_ak_[0-9a-f]{32}$`)
var agentNoncePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func (store *Store) PutAgentKey(ctx context.Context, key domain.AgentKey) error {
	now := store.nowUTC()
	if !agentIDPattern.MatchString(key.ID) || key.UserID <= 0 || len(key.SigningKey) != 32 ||
		key.Name != strings.TrimSpace(key.Name) || key.Name == "" || !utf8.ValidString(key.Name) || utf8.RuneCountInString(key.Name) > 80 || strings.ContainsFunc(key.Name, unicode.IsControl) ||
		!validAuthLifetime(key.CreatedAt, key.ExpiresAt, now, 366*24*time.Hour) {
		return fmt.Errorf("%w: invalid agent key", corestore.ErrInvalid)
	}
	seen := map[string]bool{}
	for _, scope := range key.Scopes {
		if (scope != "repositories:read" && scope != "watchlist:read") || seen[scope] {
			return fmt.Errorf("%w: invalid agent scope", corestore.ErrInvalid)
		}
		seen[scope] = true
	}
	if len(seen) == 0 {
		return fmt.Errorf("%w: no agent scopes", corestore.ErrInvalid)
	}
	scopes, _ := json.Marshal(key.Scopes)
	return store.authTransaction(ctx, func(conn *sql.Conn) error {
		var active, total, recent int
		err := conn.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(SUM(CASE WHEN revoked_at IS NULL AND expires_at>? THEN 1 ELSE 0 END),0),COALESCE(SUM(CASE WHEN created_at>? THEN 1 ELSE 0 END),0) FROM agent_keys WHERE github_user_id=?`, authTime(now), authTime(now.Add(-time.Hour)), key.UserID).Scan(&total, &active, &recent)
		if err != nil {
			return err
		}
		if active >= 10 || recent >= 20 || total >= 100 {
			return domain.ErrAgentKeyCapacity
		}
		_, err = conn.ExecContext(ctx, `INSERT INTO agent_keys(id,github_user_id,name,scopes,signing_key,created_at,expires_at) VALUES(?,?,?,?,?,?,?)`, key.ID, key.UserID, key.Name, string(scopes), key.SigningKey, authTime(key.CreatedAt), authTime(key.ExpiresAt))
		return err
	})
}

func scanAgentKey(row interface{ Scan(...any) error }) (domain.AgentKey, error) {
	var key domain.AgentKey
	var scopes, created, expires string
	var used, revoked sql.NullString
	if err := row.Scan(&key.ID, &key.UserID, &key.Login, &key.Name, &scopes, &key.SigningKey, &created, &expires, &used, &revoked); err != nil {
		return key, mapNotFound(err)
	}
	if err := json.Unmarshal([]byte(scopes), &key.Scopes); err != nil {
		return domain.AgentKey{}, err
	}
	var err error
	if key.CreatedAt, err = parseStoredTime(created); err != nil {
		return domain.AgentKey{}, err
	}
	if key.ExpiresAt, err = parseStoredTime(expires); err != nil {
		return domain.AgentKey{}, err
	}
	if key.LastUsedAt, err = parseNullableStoredTime(used); err != nil {
		return domain.AgentKey{}, err
	}
	key.RevokedAt, err = parseNullableStoredTime(revoked)
	return key, err
}

const agentKeyColumns = `k.id,k.github_user_id,u.login,k.name,k.scopes,k.signing_key,k.created_at,k.expires_at,k.last_used_at,k.revoked_at`

func (store *Store) GetAgentKey(ctx context.Context, id string) (domain.AgentKey, error) {
	if !agentIDPattern.MatchString(id) {
		return domain.AgentKey{}, corestore.ErrNotFound
	}
	return scanAgentKey(store.db.QueryRowContext(ctx, `SELECT `+agentKeyColumns+` FROM agent_keys k JOIN users u ON u.github_user_id=k.github_user_id WHERE k.id=?`, id))
}

func (store *Store) ListAgentKeys(ctx context.Context, userID int64) ([]domain.AgentKey, error) {
	if userID <= 0 {
		return nil, corestore.ErrInvalid
	}
	rows, err := store.db.QueryContext(ctx, `SELECT `+agentKeyColumns+` FROM agent_keys k JOIN users u ON u.github_user_id=k.github_user_id WHERE k.github_user_id=? ORDER BY k.created_at DESC,k.id DESC LIMIT 100`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	keys := make([]domain.AgentKey, 0)
	for rows.Next() {
		key, err := scanAgentKey(rows)
		if err != nil {
			return nil, err
		}
		key.SigningKey = nil
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

func (store *Store) RevokeAgentKey(ctx context.Context, userID int64, id string, now time.Time) error {
	if userID <= 0 || !agentIDPattern.MatchString(id) || !validAuthTime(now) {
		return corestore.ErrNotFound
	}
	result, err := store.db.ExecContext(ctx, `UPDATE agent_keys SET revoked_at=COALESCE(revoked_at,?) WHERE id=? AND github_user_id=?`, authTime(now), id, userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return corestore.ErrNotFound
	}
	return nil
}

// AcceptAgentRequest serializes revocation, replay protection, and a rolling
// per-key rate limit across processes and restarts. Failed requests consume no
// nonce; an authenticated request consumes one even if its query later fails.
func (store *Store) AcceptAgentRequest(ctx context.Context, id, nonce string, now, expires time.Time) error {
	if !agentIDPattern.MatchString(id) || !agentNoncePattern.MatchString(nonce) || !validAuthTime(now) || !expires.After(now) || expires.Sub(now) > 11*time.Minute {
		return corestore.ErrInvalid
	}
	return store.authTransaction(ctx, func(conn *sql.Conn) error {
		if _, err := conn.ExecContext(ctx, `DELETE FROM agent_request_nonces WHERE expires_at<=?`, authTime(now)); err != nil {
			return err
		}
		var valid bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_keys WHERE id=? AND revoked_at IS NULL AND created_at<=? AND expires_at>?)`, id, authTime(now), authTime(now)).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return corestore.ErrNotFound
		}
		var replay bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM agent_request_nonces WHERE key_id=? AND nonce=?)`, id, nonce).Scan(&replay); err != nil {
			return err
		}
		if replay {
			return domain.ErrAgentReplay
		}
		var count int
		if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_request_nonces WHERE key_id=? AND accepted_at>?`, id, authTime(now.Add(-time.Minute))).Scan(&count); err != nil {
			return err
		}
		if count >= 120 {
			return domain.ErrAgentRateLimited
		}
		if _, err := conn.ExecContext(ctx, `INSERT INTO agent_request_nonces(key_id,nonce,accepted_at,expires_at) VALUES(?,?,?,?)`, id, nonce, authTime(now), authTime(expires)); err != nil {
			return err
		}
		_, err := conn.ExecContext(ctx, `UPDATE agent_keys SET last_used_at=? WHERE id=?`, authTime(now), id)
		return err
	})
}

package domain

import (
	"errors"
	"time"
)

var (
	ErrAgentKeyCapacity = errors.New("agent access: key limit reached")
	ErrAgentReplay      = errors.New("agent access: replayed request")
	ErrAgentRateLimited = errors.New("agent access: rate limit reached")
)

// AgentKey is safe to show its owner. SigningKey is credential-equivalent and
// must never appear in JSON, logs, exports, or account listings.
type AgentKey struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"-"`
	Login      string     `json:"-"`
	Name       string     `json:"name"`
	Scopes     []string   `json:"scopes"`
	SigningKey []byte     `json:"-"`
	CreatedAt  time.Time  `json:"created_at"`
	ExpiresAt  time.Time  `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
}

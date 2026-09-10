package domain

import (
	"errors"
	"time"
)

var ErrAuthCapacity = errors.New("auth: pending login capacity reached")

// OAuthLoginState binds one PKCE exchange to the browser that initiated it.
// Authentication secrets must never appear in ordinary JSON DTOs or exports.
type OAuthLoginState struct {
	StateHash   string    `json:"-"`
	BindingHash string    `json:"-"`
	Verifier    string    `json:"-"`
	ReturnPath  string    `json:"return_path"`
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// AuthSession contains local authorization state, not a GitHub OAuth token.
// Only the hash of the browser's separate session token is stored in SQLite.
type AuthSession struct {
	GitHubUserID int64     `json:"github_user_id"`
	Login        string    `json:"login"`
	Admin        bool      `json:"admin"`
	CSRFToken    string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

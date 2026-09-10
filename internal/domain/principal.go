package domain

import (
	"context"
	"errors"
)

const UserRepositoryLimit = 5000

var ErrUserRepositoryLimit = errors.New("account: personal repository limit reached")

// Principal is an authenticated GitHub identity. UserID is GitHub's immutable
// numeric ID; a login rename must never transfer ownership to another account.
type Principal struct {
	UserID int64
	Login  string
	Admin  bool
}

type principalKey struct{}

func WithPrincipal(ctx context.Context, principal Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// An explicit zero principal represents a public request. A missing principal
// is reserved for trusted internal jobs and command-line operations.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(Principal)
	return principal, ok
}

type UserRepositoryState struct {
	RepositoryID int64
	IsFocus      bool
	Note         string
}

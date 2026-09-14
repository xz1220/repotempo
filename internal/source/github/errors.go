package github

import (
	"errors"
	"net/http"
)

// IsAccessBlocked identifies API authentication, permission, and rate-limit
// failures after the client's configured retries have finished. Callers should
// stop the remaining API work for this batch instead of repeating the same
// failure for every repository. An explicit repository block affects only
// that resource. An HTML page error is not a GitHub API failure.
func IsAccessBlocked(err error) bool {
	var apiError *APIError
	if !errors.As(err, &apiError) || apiError == nil {
		return false
	}
	if apiError.StatusCode == http.StatusForbidden && apiError.Code == CodeRepositoryBlocked {
		return false
	}
	switch apiError.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests:
		return true
	default:
		return false
	}
}

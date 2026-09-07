package github

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsAccessBlockedOnlyMatchesGitHubAPIAccessFailures(t *testing.T) {
	for _, status := range []int{401, 403, 429} {
		err := &APIError{StatusCode: status}
		for _, wrapped := range []error{err, fmt.Errorf("resolve repository: %w", err), errors.Join(errors.New("other failure"), err)} {
			if !IsAccessBlocked(wrapped) {
				t.Errorf("API HTTP %d was not recognized", status)
			}
		}
	}
	var nilAPIError *APIError
	for _, err := range []error{nil, nilAPIError, &APIError{StatusCode: 404}, &APIError{StatusCode: 422}, &APIError{StatusCode: 503}, errors.New("Trending HTTP 403 Forbidden"), errors.New("HTTP 429 Too Many Requests"), &TransportError{Code: CodeTimeout, Err: errors.New("timeout")}} {
		if IsAccessBlocked(err) {
			t.Errorf("non-blocking error classified as a batch access failure: %T", err)
		}
	}
}

package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRepositoryBlockedResponseDoesNotStopOtherRepositories(t *testing.T) {
	for _, tc := range []struct {
		name, message, remaining, retryAfter string
		status                               int
		wantCode                             ErrorCode
		stop                                 bool
	}{
		{"blocked", "Repository access blocked", "4927", "", 403, "repository_access_blocked", false},
		{"blocked without quota headers", "Repository access blocked", "", "", 403, "repository_access_blocked", false},
		{"unknown permission", "Resource not accessible by integration", "4927", "", 403, CodeForbidden, true},
		{"primary limit wins", "Repository access blocked", "0", "", 403, CodePrimaryRateLimit, true},
		{"secondary limit", "You have exceeded a secondary rate limit", "4927", "", 403, CodeSecondaryRateLimit, true},
		{"retry-after limit wins", "Repository access blocked", "4927", "60", 403, CodeSecondaryRateLimit, true},
		{"unauthorized", "Repository access blocked", "4927", "", 401, CodeBadRequest, true},
		{"too many requests", "Repository access blocked", "4927", "", 429, CodeSecondaryRateLimit, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/repositories/42" {
					t.Errorf("unexpected path %s", r.URL.Path)
				}
				if tc.remaining != "" {
					w.Header().Set("X-RateLimit-Remaining", tc.remaining)
				}
				if tc.retryAfter != "" {
					w.Header().Set("Retry-After", tc.retryAfter)
				}
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"message":%q}`, tc.message)
			}))
			defer server.Close()
			client := githubTestClient(t, server, nil)
			_, err := client.FetchRepositoryByID(context.Background(), 42, "")
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code != tc.wantCode {
				t.Fatalf("code=%v want=%s", err, tc.wantCode)
			}
			if IsAccessBlocked(fmt.Errorf("fetch: %w", err)) != tc.stop {
				t.Fatalf("wrong batch stop decision: %v", err)
			}
		})
	}
}

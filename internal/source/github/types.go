package github

import (
	"fmt"
	"net/http"
	"time"

	"github.com/xz1220/github-radar/internal/source"
)

const (
	SearchPerPage  = 100
	SearchMaxItems = 1000
)

type Resource string

const (
	ResourceCore   Resource = "core"
	ResourceSearch Resource = "search"
)

type RateLimit struct {
	Resource   Resource
	Limit      int
	Remaining  int
	Used       int
	Reset      time.Time
	RetryAfter time.Duration
}

type RepositoryRequest struct {
	FullName   string
	ExpectedID int64
	ETag       string
}

type RepositoryResult struct {
	Repository     source.Repository
	HTTPStatus     int
	ETag           string
	NotModified    bool
	Redirected     bool
	RedirectedFrom string
	RateLimit      RateLimit
}

type Query struct {
	Text     string
	Stars    IntRange
	Topic    string
	Created  TimeRange
	Pushed   TimeRange
	Language string
	Fork     *bool
	Archived *bool
}

type IntRange struct {
	Min *int64
	Max *int64
}

type TimeRange struct {
	From *time.Time
	To   *time.Time
}

type SearchHit struct {
	Repository source.Repository
	Profile    string
	Query      string
	// QueryRank is only the position within this Search API query. It is not
	// GitHub Trending rank and is not globally comparable after partitioning.
	QueryRank       int
	MatchedQueries  []string
	MatchedProfiles []string
}

type QueryReport struct {
	Query             string
	Depth             int
	TotalCount        int
	IncompleteResults bool
	Pages             int
	Split             bool
	Truncated         bool
	RateLimit         RateLimit
}

type SearchResult struct {
	Profile           string
	Hits              []SearchHit
	Reports           []QueryReport
	TotalCount        int
	IncompleteResults bool
	Truncated         bool
	RateLimit         RateLimit
}

type ErrorCode string

const (
	CodeBadRequest           ErrorCode = "bad_request"
	CodeNotModified          ErrorCode = "not_modified_without_cache"
	CodeForbidden            ErrorCode = "forbidden"
	CodeNotFoundOrPrivate    ErrorCode = "not_found_or_private"
	CodeValidation           ErrorCode = "validation_failed"
	CodePrimaryRateLimit     ErrorCode = "primary_rate_limit"
	CodeSecondaryRateLimit   ErrorCode = "secondary_rate_limit"
	CodeUpstream             ErrorCode = "upstream_error"
	CodeTimeout              ErrorCode = "timeout"
	CodeCanceled             ErrorCode = "canceled"
	CodeTransport            ErrorCode = "transport_error"
	CodeUnexpectedRedirect   ErrorCode = "unexpected_redirect"
	CodeRepositoryIDMismatch ErrorCode = "repository_id_mismatch"
	CodeSearchLimit          ErrorCode = "search_1000_limit"
	CodeIncompleteResults    ErrorCode = "incomplete_results"
)

type APIError struct {
	Code       ErrorCode
	StatusCode int
	Message    string
	Path       string
	RateLimit  RateLimit
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("github API %s (%d) for %s", e.Code, e.StatusCode, e.Path)
	}
	return fmt.Sprintf("github API %s (%d) for %s: %s", e.Code, e.StatusCode, e.Path, e.Message)
}

func (e *APIError) Temporary() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500 || e.Code == CodePrimaryRateLimit || e.Code == CodeSecondaryRateLimit
}

type TransportError struct {
	Code ErrorCode
	Err  error
}

func (e *TransportError) Error() string { return fmt.Sprintf("github request %s: %v", e.Code, e.Err) }
func (e *TransportError) Unwrap() error { return e.Err }

type RepositoryIDMismatchError struct {
	Expected int64
	Actual   int64
	FullName string
}

func (e *RepositoryIDMismatchError) Error() string {
	return fmt.Sprintf("github repository ID mismatch for %s: expected %d, got %d", e.FullName, e.Expected, e.Actual)
}

type SearchIntegrityError struct {
	Incomplete bool
	Truncated  bool
	Queries    []string
}

func (e *SearchIntegrityError) Error() string {
	switch {
	case e.Incomplete && e.Truncated:
		return "github search returned incomplete results and one or more queries exceeded the 1,000-result boundary"
	case e.Incomplete:
		return "github search returned incomplete_results=true"
	default:
		return "one or more GitHub search queries exceeded the 1,000-result boundary"
	}
}

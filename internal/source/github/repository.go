package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/source"
)

type githubRepository struct {
	ID              int64    `json:"id"`
	NodeID          string   `json:"node_id"`
	FullName        string   `json:"full_name"`
	HTMLURL         string   `json:"html_url"`
	Description     *string  `json:"description"`
	Language        *string  `json:"language"`
	StargazersCount int64    `json:"stargazers_count"`
	ForksCount      int64    `json:"forks_count"`
	Fork            bool     `json:"fork"`
	Archived        bool     `json:"archived"`
	Private         bool     `json:"private"`
	Topics          []string `json:"topics"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
	PushedAt        string   `json:"pushed_at"`
}

func (c *Client) FetchRepository(ctx context.Context, request RepositoryRequest) (RepositoryResult, error) {
	segments := strings.Split(request.FullName, "/")
	if len(segments) != 2 || segments[0] == "" || segments[1] == "" {
		return RepositoryResult{}, errors.New("repository full name must be owner/name")
	}
	path := "repos/" + url.PathEscape(segments[0]) + "/" + url.PathEscape(segments[1])
	target, err := c.resolve(path)
	if err != nil {
		return RepositoryResult{}, err
	}
	return c.fetchRepository(ctx, target, request)
}

// FetchRepositoryByID uses GitHub's permanent repository ID route. It is useful
// for snapshots after a rename or organization transfer.
func (c *Client) FetchRepositoryByID(ctx context.Context, id int64, etag string) (RepositoryResult, error) {
	if id <= 0 {
		return RepositoryResult{}, errors.New("repository ID must be positive")
	}
	target, err := c.resolve("repositories/" + strconv.FormatInt(id, 10))
	if err != nil {
		return RepositoryResult{}, err
	}
	return c.fetchRepository(ctx, target, RepositoryRequest{ExpectedID: id, ETag: etag})
}

// ResolveRepository implements the resolver shape used by the manual adapter.
func (c *Client) ResolveRepository(ctx context.Context, fullName string, expectedID int64) (source.Repository, error) {
	result, err := c.FetchRepository(ctx, RepositoryRequest{FullName: fullName, ExpectedID: expectedID})
	return result.Repository, err
}

func (c *Client) fetchRepository(ctx context.Context, target *url.URL, request RepositoryRequest) (RepositoryResult, error) {
	headers := make(http.Header)
	if request.ETag != "" {
		headers.Set("If-None-Match", request.ETag)
	}
	response, err := c.do(ctx, target, ResourceCore, headers)
	if err != nil {
		return RepositoryResult{}, err
	}
	result := RepositoryResult{
		HTTPStatus: response.StatusCode,
		ETag:       response.Header.Get("ETag"),
		RateLimit:  response.RateLimit,
	}
	if result.ETag == "" {
		result.ETag = request.ETag
	}
	if response.StatusCode == http.StatusNotModified {
		result.NotModified = true
		return result, nil
	}
	if response.StatusCode == http.StatusMovedPermanently {
		location := response.Header.Get("Location")
		if location == "" {
			return result, &APIError{Code: CodeUnexpectedRedirect, StatusCode: response.StatusCode, Message: "permanent redirect omitted Location", Path: target.EscapedPath(), RateLimit: response.RateLimit}
		}
		reference, parseErr := url.Parse(location)
		if parseErr != nil {
			return result, &APIError{Code: CodeUnexpectedRedirect, StatusCode: response.StatusCode, Message: "invalid redirect Location", Path: target.EscapedPath(), RateLimit: response.RateLimit}
		}
		redirectTarget := target.ResolveReference(reference)
		if redirectTarget.Scheme != c.baseURL.Scheme || !strings.EqualFold(redirectTarget.Host, c.baseURL.Host) {
			return result, &APIError{Code: CodeUnexpectedRedirect, StatusCode: response.StatusCode, Message: "permanent redirect points to a different origin", Path: target.EscapedPath(), RateLimit: response.RateLimit}
		}
		followed, followErr := c.do(ctx, redirectTarget, ResourceCore, headers)
		if followErr != nil {
			return result, followErr
		}
		result.Redirected = true
		result.RedirectedFrom = request.FullName
		result.HTTPStatus = followed.StatusCode
		result.RateLimit = followed.RateLimit
		if etag := followed.Header.Get("ETag"); etag != "" {
			result.ETag = etag
		}
		response = followed
		if response.StatusCode == http.StatusMovedPermanently {
			return result, &APIError{Code: CodeUnexpectedRedirect, StatusCode: response.StatusCode, Message: "repository returned more than one permanent redirect", Path: redirectTarget.EscapedPath(), RateLimit: response.RateLimit}
		}
		if response.StatusCode == http.StatusNotModified {
			result.NotModified = true
			return result, nil
		}
	}
	if response.StatusCode != http.StatusOK {
		return result, apiError(response)
	}

	var payload githubRepository
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return result, fmt.Errorf("decode GitHub repository response: %w", err)
	}
	if payload.ID <= 0 || payload.FullName == "" {
		return result, errors.New("GitHub repository response omitted id or full_name")
	}
	repository, err := payload.toSource()
	if err != nil {
		return result, err
	}
	result.Repository = repository
	if request.ExpectedID > 0 && payload.ID != request.ExpectedID {
		return result, &RepositoryIDMismatchError{Expected: request.ExpectedID, Actual: payload.ID, FullName: payload.FullName}
	}
	return result, nil
}

func (r githubRepository) toSource() (source.Repository, error) {
	createdAt, err := parseOptionalGitHubTime(r.CreatedAt)
	if err != nil {
		return source.Repository{}, fmt.Errorf("parse created_at for %s: %w", r.FullName, err)
	}
	updatedAt, err := parseOptionalGitHubTime(r.UpdatedAt)
	if err != nil {
		return source.Repository{}, fmt.Errorf("parse updated_at for %s: %w", r.FullName, err)
	}
	pushedAt, err := parseOptionalGitHubTime(r.PushedAt)
	if err != nil {
		return source.Repository{}, fmt.Errorf("parse pushed_at for %s: %w", r.FullName, err)
	}
	description := ""
	if r.Description != nil {
		description = *r.Description
	}
	language := ""
	if r.Language != nil {
		language = *r.Language
	}
	stars := r.StargazersCount
	forks := r.ForksCount
	githubStatus := "active"
	if r.Private {
		githubStatus = "private"
	} else if r.Archived {
		githubStatus = "archived"
	}
	return source.Repository{
		ID:            r.ID,
		NodeID:        r.NodeID,
		FullName:      r.FullName,
		HTMLURL:       r.HTMLURL,
		Description:   description,
		Language:      language,
		AbsoluteStars: &stars,
		Forks:         &forks,
		Fork:          r.Fork,
		Archived:      r.Archived,
		Private:       r.Private,
		GitHubStatus:  githubStatus,
		Topics:        source.CloneTags(r.Topics),
		CreatedAt:     createdAt,
		UpdatedAt:     updatedAt,
		PushedAt:      pushedAt,
	}, nil
}

func parseOptionalGitHubTime(value string) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}

package ossinsight

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/xz1220/repotempo/internal/config"
	"github.com/xz1220/repotempo/internal/source"
)

type ClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	Now        func() time.Time
}

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
	now        func() time.Time
}

type Signal struct {
	Window            string
	Period            string
	Rank              int
	WindowStars       *int64
	Forks             *int64
	PullRequests      *int64
	Pushes            *int64
	TotalScore        *float64
	ContributorLogins []string
	CollectionNames   []string
	CapturedAt        time.Time
}

type Repository struct {
	Repository source.Repository
	Signals    map[string]Signal
}

type Result struct {
	Repositories []Repository
	Warnings     []source.Warning
	Columns      []string
	RowsByWindow map[string]int
}

type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("OSS Insight API returned %d: %s", e.StatusCode, e.Message)
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL, err := url.Parse(options.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid OSS Insight base URL %q", options.BaseURL)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("OSS Insight base URL must use HTTP or HTTPS")
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	copyClient := *httpClient
	if options.Timeout > 0 {
		copyClient.Timeout = options.Timeout
	} else if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &Client{baseURL: baseURL, httpClient: &copyClient, now: options.Now}, nil
}

func (c *Client) FetchWindows(ctx context.Context, windows []config.OSSWindow, language string) (Result, error) {
	result := Result{RowsByWindow: make(map[string]int)}
	index := make(map[int64]int)
	for _, window := range windows {
		rows, columns, warnings, err := c.fetchWindow(ctx, window, language)
		if err != nil {
			return result, fmt.Errorf("fetch OSS Insight window %q: %w", window.Name, err)
		}
		if len(result.Columns) == 0 {
			result.Columns = columns
		}
		result.RowsByWindow[window.Name] = len(rows)
		result.Warnings = append(result.Warnings, warnings...)
		seenWindow := make(map[int64]struct{})
		for rowIndex, row := range rows {
			if _, duplicate := seenWindow[row.Repository.ID]; duplicate {
				result.Warnings = append(result.Warnings, source.Warning{Row: rowIndex + 1, Code: "duplicate_repository", Message: fmt.Sprintf("repository ID %d repeated in window %s", row.Repository.ID, window.Name)})
				continue
			}
			seenWindow[row.Repository.ID] = struct{}{}
			position, exists := index[row.Repository.ID]
			if !exists {
				row.Signals = cloneSignals(row.Signals)
				index[row.Repository.ID] = len(result.Repositories)
				result.Repositories = append(result.Repositories, row)
				continue
			}
			current := &result.Repositories[position]
			if current.Repository.FullName != row.Repository.FullName && row.Repository.FullName != "" {
				current.Repository.FullName = row.Repository.FullName
			}
			if current.Repository.Description == "" {
				current.Repository.Description = row.Repository.Description
			}
			if current.Repository.Language == "" {
				current.Repository.Language = row.Repository.Language
			}
			for name, signal := range row.Signals {
				current.Signals[name] = signal
			}
		}
	}
	return result, nil
}

func (r Result) Candidates(discoveredAt time.Time) []source.Candidate {
	candidates := make([]source.Candidate, 0, len(r.Repositories))
	for _, repository := range r.Repositories {
		candidates = append(candidates, source.Candidate{
			Repository:   repository.Repository,
			Source:       "ossinsight",
			DiscoveredAt: discoveredAt,
		})
	}
	return candidates
}

type envelope struct {
	Data struct {
		Columns []struct {
			Column string `json:"col"`
		} `json:"columns"`
		Rows []map[string]json.RawMessage `json:"rows"`
	} `json:"data"`
}

func (c *Client) fetchWindow(ctx context.Context, window config.OSSWindow, language string) ([]Repository, []string, []source.Warning, error) {
	target := *c.baseURL
	values := target.Query()
	values.Set("period", window.Period)
	values.Set("language", language)
	target.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return nil, nil, nil, err
	}
	request.Header.Set("Accept", "application/json")
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, nil, nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, nil, nil, err
	}
	if response.StatusCode != http.StatusOK {
		message := strings.TrimSpace(string(body))
		if len(message) > 300 {
			message = message[:300]
		}
		return nil, nil, nil, &APIError{StatusCode: response.StatusCode, Message: message}
	}
	var payload envelope
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, nil, nil, fmt.Errorf("decode OSS Insight response: %w", err)
	}
	columns := make([]string, 0, len(payload.Data.Columns))
	for _, column := range payload.Data.Columns {
		columns = append(columns, column.Column)
	}
	capturedAt := c.now().UTC()
	repositories := make([]Repository, 0, len(payload.Data.Rows))
	warnings := make([]source.Warning, 0)
	for index, raw := range payload.Data.Rows {
		repository, rowWarnings, rowErr := parseRow(raw, window, index+1, capturedAt)
		warnings = append(warnings, rowWarnings...)
		if rowErr != nil {
			warnings = append(warnings, source.Warning{Row: index + 1, Code: "invalid_row", Message: rowErr.Error()})
			continue
		}
		repositories = append(repositories, repository)
	}
	return repositories, columns, warnings, nil
}

func parseRow(raw map[string]json.RawMessage, window config.OSSWindow, rank int, capturedAt time.Time) (Repository, []source.Warning, error) {
	warnings := make([]source.Warning, 0)
	repoID, present, err := parseOptionalInt(raw["repo_id"])
	if err != nil || !present || repoID <= 0 {
		return Repository{}, warnings, errors.New("repo_id must be a positive integer")
	}
	fullName, err := parseString(raw["repo_name"])
	if err != nil || !validFullName(fullName) {
		return Repository{}, warnings, errors.New("repo_name must be owner/name")
	}
	language, _ := parseString(raw["primary_language"])
	description, _ := parseString(raw["description"])
	windowStars := optionalIntField(raw, "stars", rank, &warnings)
	forks := optionalIntField(raw, "forks", rank, &warnings)
	pullRequests := optionalIntField(raw, "pull_requests", rank, &warnings)
	pushes := optionalIntField(raw, "pushes", rank, &warnings)
	totalScore := optionalFloatField(raw, "total_score", rank, &warnings)
	contributors, _ := parseString(raw["contributor_logins"])
	collections, _ := parseString(raw["collection_names"])
	signal := Signal{
		Window:            window.Name,
		Period:            window.Period,
		Rank:              rank,
		WindowStars:       windowStars,
		Forks:             forks,
		PullRequests:      pullRequests,
		Pushes:            pushes,
		TotalScore:        totalScore,
		ContributorLogins: splitList(contributors),
		CollectionNames:   splitList(collections),
		CapturedAt:        capturedAt,
	}
	return Repository{
		Repository: source.Repository{
			ID:          repoID,
			FullName:    fullName,
			HTMLURL:     "https://github.com/" + fullName,
			Description: description,
			Language:    language,
			// AbsoluteStars must stay nil: OSS Insight stars is a rolling-window delta.
			AbsoluteStars: nil,
		},
		Signals: map[string]Signal{window.Name: signal},
	}, warnings, nil
}

func optionalIntField(raw map[string]json.RawMessage, field string, row int, warnings *[]source.Warning) *int64 {
	value, present, err := parseOptionalInt(raw[field])
	if err != nil {
		*warnings = append(*warnings, source.Warning{Row: row, Code: "invalid_" + field, Message: err.Error()})
		return nil
	}
	if !present {
		return nil
	}
	return &value
}

func optionalFloatField(raw map[string]json.RawMessage, field string, row int, warnings *[]source.Warning) *float64 {
	value, present, err := parseOptionalFloat(raw[field])
	if err != nil {
		*warnings = append(*warnings, source.Warning{Row: row, Code: "invalid_" + field, Message: err.Error()})
		return nil
	}
	if !present {
		return nil
	}
	return &value
}

func parseString(raw json.RawMessage) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value, nil
	}
	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		return number.String(), nil
	}
	return "", errors.New("field must be a string or number")
}

func parseOptionalInt(raw json.RawMessage) (int64, bool, error) {
	value, err := parseString(raw)
	if err != nil {
		return 0, false, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%q is not an integer", value)
	}
	return parsed, true, nil
}

func parseOptionalFloat(raw json.RawMessage) (float64, bool, error) {
	value, err := parseString(raw)
	if err != nil {
		return 0, false, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, false, fmt.Errorf("%q is not a number", value)
	}
	return parsed, true, nil
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func validFullName(value string) bool {
	parts := strings.Split(value, "/")
	return len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func cloneSignals(input map[string]Signal) map[string]Signal {
	result := make(map[string]Signal, len(input))
	for name, signal := range input {
		signal.ContributorLogins = append([]string(nil), signal.ContributorLogins...)
		signal.CollectionNames = append([]string(nil), signal.CollectionNames...)
		result[name] = signal
	}
	return result
}

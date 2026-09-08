package github

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
)

type activityRepository struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
	Private       *bool  `json:"private"`
	Fork          *bool  `json:"fork"`
}

type activityCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Committer *struct {
			Date string `json:"date"`
		} `json:"committer"`
	} `json:"commit"`
}

// FetchActivity observes default-branch history at one pinned commit. Counts
// include merges, bots and (for forks) inherited history; they do not measure
// developer effort. Only a successful response may replace a cached result.
// The 30 UTC dates include today up to now. Pagination is bounded, and a
// successful but truncated observation is explicitly marked Complete=false.
func (c *Client) FetchActivity(ctx context.Context, repositoryID int64, now time.Time, maxPages int) (domain.RepositoryActivity, error) {
	if repositoryID <= 0 || maxPages < 1 || maxPages > 5 {
		return domain.RepositoryActivity{}, errors.New("activity requires a positive repository ID and maxPages between 1 and 5")
	}
	end := now.UTC()
	if now.IsZero() || end.Year() < 1970 || end.Year() > 2099 || c.now().Before(end) {
		return domain.RepositoryActivity{}, errors.New("activity cutoff must be a valid non-future GitHub timestamp")
	}
	start := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -29)
	result := domain.RepositoryActivity{RepositoryID: repositoryID, WindowStart: start, WindowEnd: end, Daily: make([]domain.ActivityDay, 30)}
	for i := range result.Daily {
		result.Daily[i].Date = start.AddDate(0, 0, i).Format(time.DateOnly)
	}

	metaURL, err := c.resolve("repositories/" + strconv.FormatInt(repositoryID, 10))
	if err != nil {
		return domain.RepositoryActivity{}, err
	}
	response, err := c.activityRequest(ctx, metaURL)
	if err != nil {
		return domain.RepositoryActivity{}, err
	}
	if response.StatusCode != http.StatusOK {
		return domain.RepositoryActivity{}, apiError(response)
	}
	var repository activityRepository
	if err := json.Unmarshal(response.Body, &repository); err != nil {
		return domain.RepositoryActivity{}, fmt.Errorf("decode activity repository: %w", err)
	}
	if repository.ID != repositoryID {
		return domain.RepositoryActivity{}, &RepositoryIDMismatchError{Expected: repositoryID, Actual: repository.ID, FullName: repository.FullName}
	}
	if repository.Private == nil || repository.Fork == nil || !activityFullNameValid(repository.FullName) || !activityBranchValid(repository.DefaultBranch) {
		return domain.RepositoryActivity{}, errors.New("activity repository omitted valid identity, visibility or default branch")
	}
	if *repository.Private {
		return domain.RepositoryActivity{}, &APIError{Code: CodeNotFoundOrPrivate, StatusCode: http.StatusForbidden, Message: "activity is limited to public repositories", Path: metaURL.Path}
	}
	result.DefaultBranch, result.IsFork = repository.DefaultBranch, *repository.Fork

	commitsURL, err := c.resolve("repos/" + repository.FullName + "/commits")
	if err != nil {
		return domain.RepositoryActivity{}, err
	}
	query := url.Values{"sha": {repository.DefaultBranch}, "per_page": {"1"}, "until": {end.Format(time.RFC3339Nano)}}
	commitsURL.RawQuery = query.Encode()
	response, err = c.activityRequest(ctx, commitsURL)
	if err != nil {
		return domain.RepositoryActivity{}, err
	}
	if activityEmptyRepository(response) {
		result.Complete = true
		return c.finishActivity(result)
	}
	if response.StatusCode != http.StatusOK {
		return domain.RepositoryActivity{}, apiError(response)
	}
	head, err := decodeActivityCommits(response.Body, 1)
	if err != nil || len(head) != 1 {
		return domain.RepositoryActivity{}, errors.New("activity branch response did not contain exactly one valid commit")
	}
	latest, err := head[0].committerTime()
	if err != nil || latest.After(end) {
		return domain.RepositoryActivity{}, errors.New("activity branch commit has an invalid or future committer date")
	}
	result.HeadSHA, result.LatestCommitAt = head[0].SHA, &latest
	query.Set("sha", result.HeadSHA)
	query.Set("since", start.Format(time.RFC3339Nano))
	query.Set("per_page", "100")
	seen := make(map[string]time.Time)
	for page := 1; page <= maxPages; page++ {
		query.Set("page", strconv.Itoa(page))
		commitsURL.RawQuery = query.Encode()
		response, err = c.activityRequest(ctx, commitsURL)
		if err != nil {
			return domain.RepositoryActivity{}, err
		}
		if response.StatusCode != http.StatusOK {
			return domain.RepositoryActivity{}, apiError(response)
		}
		commits, err := decodeActivityCommits(response.Body, 100)
		if err != nil {
			return domain.RepositoryActivity{}, fmt.Errorf("decode activity page %d: %w", page, err)
		}
		for _, commit := range commits {
			at, err := commit.committerTime()
			if err != nil || at.Before(start) || at.After(end) {
				return domain.RepositoryActivity{}, fmt.Errorf("activity page %d contains an invalid or out-of-window committer date", page)
			}
			if previous, exists := seen[commit.SHA]; exists {
				if !previous.Equal(at) {
					return domain.RepositoryActivity{}, errors.New("activity repeated a commit SHA with conflicting dates")
				}
				continue
			}
			seen[commit.SHA] = at
			day := int(at.Sub(start) / (24 * time.Hour))
			if result.Daily[day].Count == 0 {
				result.ActiveDays++
			}
			result.Daily[day].Count++
			result.Commits++
		}
		next, err := activityHasNext(response.Header.Values("Link"), commitsURL, page)
		if err != nil {
			return domain.RepositoryActivity{}, err
		}
		if next && len(commits) == 0 {
			return domain.RepositoryActivity{}, errors.New("activity empty page unexpectedly declares a next page")
		}
		if !next {
			result.Complete = true
			break
		}
	}
	if result.Commits == 0 && !latest.Before(start) {
		return domain.RepositoryActivity{}, errors.New("activity window omitted its known in-window branch commit")
	}
	return c.finishActivity(result)
}

// Optional activity enrichment must not exhaust the Core quota needed for
// essential repository snapshots, including when quota drops within one repo.
func (c *Client) activityRequest(ctx context.Context, target *url.URL) (rawResponse, error) {
	if rate, ok := c.rate(ResourceCore); ok && rate.Limit > 0 && rate.Remaining >= 0 && rate.Remaining < 100 && rate.Reset.After(c.now()) {
		return rawResponse{}, &APIError{Code: CodePrimaryRateLimit, StatusCode: http.StatusTooManyRequests, Message: "Core API quota reserved for repository snapshots", Path: target.EscapedPath(), RateLimit: rate}
	}
	return c.do(ctx, target, ResourceCore, nil)
}

func (c *Client) finishActivity(result domain.RepositoryActivity) (domain.RepositoryActivity, error) {
	result.FetchedAt = c.now().UTC()
	if result.FetchedAt.Before(result.WindowEnd) {
		return domain.RepositoryActivity{}, errors.New("activity clock moved before its cutoff")
	}
	return result, nil
}

func activityEmptyRepository(response rawResponse) bool {
	if response.StatusCode != http.StatusConflict {
		return false
	}
	var payload struct {
		Message string `json:"message"`
	}
	return json.Unmarshal(response.Body, &payload) == nil && strings.EqualFold(strings.TrimSpace(payload.Message), "Git Repository is empty.")
}

func decodeActivityCommits(body []byte, limit int) ([]activityCommit, error) {
	var commits []activityCommit
	if err := json.Unmarshal(body, &commits); err != nil {
		return nil, err
	}
	if commits == nil || len(commits) > limit {
		return nil, errors.New("expected a non-null commit array within requested page size")
	}
	for _, commit := range commits {
		if len(commit.SHA) != 40 && len(commit.SHA) != 64 {
			return nil, errors.New("commit SHA must be a full hexadecimal object ID")
		}
		if _, err := hex.DecodeString(commit.SHA); err != nil {
			return nil, errors.New("commit SHA must be hexadecimal")
		}
	}
	return commits, nil
}

func (commit activityCommit) committerTime() (time.Time, error) {
	if commit.Commit.Committer == nil {
		return time.Time{}, errors.New("commit omitted committer date")
	}
	at, err := time.Parse(time.RFC3339Nano, commit.Commit.Committer.Date)
	if err != nil || at.IsZero() {
		return time.Time{}, errors.New("commit has an invalid committer date")
	}
	return at.UTC(), nil
}

func activityFullNameValid(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[0]) > 39 || len(parts[1]) == 0 || len(parts[1]) > 100 || parts[1] == "." || parts[1] == ".." {
		return false
	}
	for i, part := range parts {
		for _, ch := range part {
			if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || i == 1 && (ch == '.' || ch == '_') {
				continue
			}
			return false
		}
	}
	return true
}

func activityBranchValid(branch string) bool {
	return branch != "" && strings.TrimSpace(branch) == branch && len(branch) <= 1024 && utf8.ValidString(branch) && strings.IndexFunc(branch, unicode.IsControl) == -1
}

// Inspect pagination metadata but always construct our own next request. Even
// a compromised Link header cannot change the origin, repository or pinned SHA.
func activityHasNext(headers []string, current *url.URL, page int) (bool, error) {
	next := false
	for _, header := range headers {
		for _, link := range strings.Split(header, ",") {
			parts := strings.Split(strings.TrimSpace(link), ";")
			if len(parts) < 2 || !strings.HasPrefix(parts[0], "<") || !strings.HasSuffix(parts[0], ">") {
				return false, errors.New("activity pagination has a malformed Link header")
			}
			ref, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(parts[0], "<"), ">"))
			if err != nil {
				return false, errors.New("activity pagination has an invalid URL")
			}
			target := current.ResolveReference(ref)
			if target.Scheme != current.Scheme || !strings.EqualFold(target.Host, current.Host) || target.User != nil || target.Fragment != "" || target.EscapedPath() != current.EscapedPath() {
				return false, errors.New("activity pagination changed origin or repository path")
			}
			values, err := url.ParseQuery(target.RawQuery)
			if err != nil || len(values) != len(current.Query()) {
				return false, errors.New("activity pagination changed query scope")
			}
			for key, original := range current.Query() {
				if len(values[key]) != 1 || key != "page" && values.Get(key) != original[0] {
					return false, errors.New("activity pagination changed query scope")
				}
			}
			linkedPage, err := strconv.Atoi(values.Get("page"))
			if err != nil || linkedPage < 1 {
				return false, errors.New("activity pagination has an invalid page")
			}
			for _, parameter := range parts[1:] {
				key, value, ok := strings.Cut(strings.TrimSpace(parameter), "=")
				if !ok || key != "rel" {
					continue
				}
				for _, relation := range strings.Fields(strings.Trim(value, "\"")) {
					if relation == "next" {
						if next || linkedPage != page+1 {
							return false, errors.New("activity pagination has an invalid next page")
						}
						next = true
					}
				}
			}
		}
	}
	return next, nil
}

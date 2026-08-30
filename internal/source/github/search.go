package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xz1220/github-radar/internal/config"
	"github.com/xz1220/github-radar/internal/source"
)

type searchPage struct {
	TotalCount        int                `json:"total_count"`
	IncompleteResults bool               `json:"incomplete_results"`
	Items             []githubRepository `json:"items"`
	RateLimit         RateLimit          `json:"-"`
}

type searchState struct {
	result      SearchResult
	hitIndex    map[int64]int
	integrity   map[string]struct{}
	profileName string
	profile     config.SearchProfile
	now         time.Time
}

func (q Query) String() string {
	parts := make([]string, 0, 8)
	if text := strings.TrimSpace(q.Text); text != "" {
		parts = append(parts, text)
	}
	if qualifier := numberQualifier("stars", q.Stars); qualifier != "" {
		parts = append(parts, qualifier)
	}
	if q.Topic != "" {
		parts = append(parts, "topic:"+quoteQualifier(q.Topic))
	}
	if qualifier := timeQualifier("created", q.Created); qualifier != "" {
		parts = append(parts, qualifier)
	}
	if qualifier := timeQualifier("pushed", q.Pushed); qualifier != "" {
		parts = append(parts, qualifier)
	}
	if q.Language != "" {
		parts = append(parts, "language:"+quoteQualifier(q.Language))
	}
	if q.Fork != nil {
		parts = append(parts, "fork:"+strconv.FormatBool(*q.Fork))
	}
	if q.Archived != nil {
		parts = append(parts, "archived:"+strconv.FormatBool(*q.Archived))
	}
	return strings.Join(parts, " ")
}

func numberQualifier(name string, value IntRange) string {
	switch {
	case value.Min != nil && value.Max != nil && *value.Min == *value.Max:
		return name + ":" + strconv.FormatInt(*value.Min, 10)
	case value.Min != nil && value.Max != nil:
		return name + ":" + strconv.FormatInt(*value.Min, 10) + ".." + strconv.FormatInt(*value.Max, 10)
	case value.Min != nil:
		return name + ":>=" + strconv.FormatInt(*value.Min, 10)
	case value.Max != nil:
		return name + ":<=" + strconv.FormatInt(*value.Max, 10)
	default:
		return ""
	}
}

func timeQualifier(name string, value TimeRange) string {
	format := func(date *time.Time) string { return date.Format("2006-01-02") }
	switch {
	case value.From != nil && value.To != nil && sameDate(*value.From, *value.To):
		return name + ":" + format(value.From)
	case value.From != nil && value.To != nil:
		return name + ":" + format(value.From) + ".." + format(value.To)
	case value.From != nil:
		return name + ":>=" + format(value.From)
	case value.To != nil:
		return name + ":<=" + format(value.To)
	default:
		return ""
	}
}

func quoteQualifier(value string) string {
	if !strings.ContainsAny(value, " \t\r\n\"") {
		return value
	}
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `"`, `\"`)
	return `"` + value + `"`
}

func sameDate(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// SearchProfile executes every structured query in a profile. Queries whose
// total_count exceeds GitHub's 1,000-item boundary are recursively partitioned
// by the configured star or date range. Partial hits are returned alongside a
// SearchIntegrityError when completeness cannot be guaranteed.
func (c *Client) SearchProfile(ctx context.Context, profile config.SearchProfile, now time.Time) (SearchResult, error) {
	state := &searchState{
		result:      SearchResult{Profile: profile.Name},
		hitIndex:    make(map[int64]int),
		integrity:   make(map[string]struct{}),
		profileName: profile.Name,
		profile:     profile,
		now:         now,
	}
	for _, configured := range profile.Queries {
		query, err := resolveQuery(configured, now)
		if err != nil {
			return state.result, fmt.Errorf("resolve search profile %q: %w", profile.Name, err)
		}
		if err := c.fetchQuery(ctx, state, query, 0); err != nil {
			return state.result, err
		}
	}
	if rates := c.RateLimits(); rates[ResourceSearch].Resource != "" {
		state.result.RateLimit = rates[ResourceSearch]
	}
	if state.result.IncompleteResults || state.result.Truncated {
		queries := make([]string, 0, len(state.integrity))
		for query := range state.integrity {
			queries = append(queries, query)
		}
		sort.Strings(queries)
		return state.result, &SearchIntegrityError{
			Incomplete: state.result.IncompleteResults,
			Truncated:  state.result.Truncated,
			Queries:    queries,
		}
	}
	return state.result, nil
}

func (c *Client) fetchQuery(ctx context.Context, state *searchState, query Query, depth int) error {
	queryText := query.String()
	page, err := c.searchPage(ctx, query, state.profile.Sort, state.profile.Order, SearchPerPage, 1)
	if err != nil {
		return fmt.Errorf("search profile %q query %q: %w", state.profileName, queryText, err)
	}
	reportIndex := len(state.result.Reports)
	state.result.Reports = append(state.result.Reports, QueryReport{
		Query:             queryText,
		Depth:             depth,
		TotalCount:        page.TotalCount,
		IncompleteResults: page.IncompleteResults,
		Pages:             1,
		RateLimit:         page.RateLimit,
	})
	if depth == 0 {
		state.result.TotalCount += page.TotalCount
	}
	if page.IncompleteResults {
		state.result.IncompleteResults = true
		state.integrity[queryText] = struct{}{}
	}

	if page.TotalCount > SearchMaxItems {
		children, splitErr := c.splitQuery(ctx, state, query, page, depth)
		if splitErr == nil && len(children) == 2 && depth < state.profile.Partition.MaxDepth {
			state.result.Reports[reportIndex].Split = true
			for _, child := range children {
				if err := c.fetchQuery(ctx, state, child, depth+1); err != nil {
					return err
				}
			}
			return nil
		}
		state.result.Truncated = true
		state.integrity[queryText] = struct{}{}
		state.result.Reports[reportIndex].Truncated = true
	}

	pages := (page.TotalCount + SearchPerPage - 1) / SearchPerPage
	if pages < 1 {
		pages = 1
	}
	if pages > SearchMaxItems/SearchPerPage {
		pages = SearchMaxItems / SearchPerPage
	}
	c.addPageHits(state, query, page, 1)
	for pageNumber := 2; pageNumber <= pages; pageNumber++ {
		next, nextErr := c.searchPage(ctx, query, state.profile.Sort, state.profile.Order, SearchPerPage, pageNumber)
		if nextErr != nil {
			return fmt.Errorf("search profile %q query %q page %d: %w", state.profileName, queryText, pageNumber, nextErr)
		}
		state.result.Reports[reportIndex].Pages = pageNumber
		state.result.Reports[reportIndex].RateLimit = next.RateLimit
		if next.IncompleteResults {
			state.result.IncompleteResults = true
			state.integrity[queryText] = struct{}{}
			state.result.Reports[reportIndex].IncompleteResults = true
		}
		if len(next.Items) == 0 {
			// A page before the computed end becoming empty usually means the
			// search index changed while paging. Keep prior hits, but never mark
			// the query complete or advance its successful schedule timestamp.
			state.result.IncompleteResults = true
			state.integrity[queryText] = struct{}{}
			state.result.Reports[reportIndex].IncompleteResults = true
			break
		}
		c.addPageHits(state, query, next, pageNumber)
	}
	return nil
}

func (c *Client) splitQuery(ctx context.Context, state *searchState, query Query, first searchPage, depth int) ([]Query, error) {
	if depth >= state.profile.Partition.MaxDepth {
		return nil, errors.New("maximum partition depth reached")
	}
	switch state.profile.Partition.By {
	case "stars":
		firstIsStarSorted := state.profile.Sort == "stars" && state.profile.Order == "desc"
		return c.splitStars(ctx, query, first, firstIsStarSorted)
	case "created":
		return splitDate(query, "created", state.now)
	case "pushed":
		return splitDate(query, "pushed", state.now)
	default:
		return nil, errors.New("partitioning disabled")
	}
}

func (c *Client) splitStars(ctx context.Context, query Query, first searchPage, firstIsStarSorted bool) ([]Query, error) {
	minimum := int64(0)
	if query.Stars.Min != nil {
		minimum = *query.Stars.Min
	}
	maximum := int64(-1)
	if query.Stars.Max != nil {
		maximum = *query.Stars.Max
	} else if firstIsStarSorted && !first.IncompleteResults && len(first.Items) > 0 {
		maximum = first.Items[0].StargazersCount
	} else {
		// A sort=stars probe makes the upper boundary exact even when the profile
		// itself sorts by updated.
		probe, err := c.searchPage(ctx, query, "stars", "desc", SearchPerPage, 1)
		if err != nil {
			return nil, fmt.Errorf("probe maximum stars: %w", err)
		}
		if probe.IncompleteResults {
			return nil, errors.New("maximum-star probe returned incomplete results")
		}
		if len(probe.Items) == 0 {
			return nil, errors.New("star probe returned no items")
		}
		maximum = probe.Items[0].StargazersCount
	}
	if maximum <= minimum {
		return nil, errors.New("star range cannot be split further")
	}
	middle := minimum + (maximum-minimum)/2
	left, right := query, query
	leftMin, leftMax := minimum, middle
	rightMin, rightMax := middle+1, maximum
	left.Stars = IntRange{Min: &leftMin, Max: &leftMax}
	right.Stars = IntRange{Min: &rightMin, Max: &rightMax}
	return []Query{left, right}, nil
}

func splitDate(query Query, field string, now time.Time) ([]Query, error) {
	rangeValue := query.Created
	if field == "pushed" {
		rangeValue = query.Pushed
	}
	if rangeValue.From == nil {
		return nil, fmt.Errorf("%s range has no lower bound", field)
	}
	from := dateOnly(*rangeValue.From)
	to := dateOnly(now)
	if rangeValue.To != nil {
		to = dateOnly(*rangeValue.To)
	}
	if !to.After(from) {
		return nil, fmt.Errorf("%s range cannot be split further", field)
	}
	fromUTC := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	toUTC := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	days := int(toUTC.Sub(fromUTC).Hours() / 24)
	middle := from.AddDate(0, 0, days/2)
	rightStart := middle.AddDate(0, 0, 1)
	left, right := query, query
	leftRange := TimeRange{From: &from, To: &middle}
	rightRange := TimeRange{From: &rightStart, To: &to}
	if field == "created" {
		left.Created, right.Created = leftRange, rightRange
	} else {
		left.Pushed, right.Pushed = leftRange, rightRange
	}
	return []Query{left, right}, nil
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func (c *Client) searchPage(ctx context.Context, query Query, sortBy, order string, perPage, page int) (searchPage, error) {
	target, err := c.resolve("search/repositories")
	if err != nil {
		return searchPage{}, err
	}
	values := make(url.Values)
	values.Set("q", query.String())
	values.Set("sort", sortBy)
	values.Set("order", order)
	values.Set("per_page", strconv.Itoa(perPage))
	values.Set("page", strconv.Itoa(page))
	target.RawQuery = values.Encode()
	response, err := c.do(ctx, target, ResourceSearch, nil)
	if err != nil {
		return searchPage{}, err
	}
	if response.StatusCode != http.StatusOK {
		return searchPage{}, apiError(response)
	}
	var payload searchPage
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return searchPage{}, fmt.Errorf("decode GitHub search response: %w", err)
	}
	if payload.TotalCount < 0 {
		return searchPage{}, errors.New("GitHub search returned negative total_count")
	}
	payload.RateLimit = response.RateLimit
	return payload, nil
}

func (c *Client) addPageHits(state *searchState, query Query, page searchPage, pageNumber int) {
	queryText := query.String()
	for index, raw := range page.Items {
		if raw.ID <= 0 || raw.FullName == "" {
			continue
		}
		if query.Fork != nil && raw.Fork != *query.Fork {
			continue
		}
		if query.Archived != nil && raw.Archived != *query.Archived {
			continue
		}
		repository, err := raw.toSource()
		if err != nil {
			continue
		}
		if hitIndex, exists := state.hitIndex[raw.ID]; exists {
			hit := &state.result.Hits[hitIndex]
			hit.MatchedQueries = appendUniqueString(hit.MatchedQueries, queryText)
			continue
		}
		hit := SearchHit{
			Repository:      repository,
			Profile:         state.profileName,
			Query:           queryText,
			QueryRank:       (pageNumber-1)*SearchPerPage + index + 1,
			MatchedQueries:  []string{queryText},
			MatchedProfiles: []string{state.profileName},
		}
		state.hitIndex[raw.ID] = len(state.result.Hits)
		state.result.Hits = append(state.result.Hits, hit)
	}
}

func resolveQuery(input config.SearchQuery, now time.Time) (Query, error) {
	createdFrom, createdTo, err := input.Created.Resolve(now)
	if err != nil {
		return Query{}, fmt.Errorf("created: %w", err)
	}
	pushedFrom, pushedTo, err := input.Pushed.Resolve(now)
	if err != nil {
		return Query{}, fmt.Errorf("pushed: %w", err)
	}
	return Query{
		Text:     input.Text,
		Stars:    IntRange{Min: cloneInt64(input.Stars.Min), Max: cloneInt64(input.Stars.Max)},
		Topic:    input.Topic,
		Created:  TimeRange{From: createdFrom, To: createdTo},
		Pushed:   TimeRange{From: pushedFrom, To: pushedTo},
		Language: input.Language,
		Fork:     cloneBool(input.Fork),
		Archived: cloneBool(input.Archived),
	}, nil
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneBool(value *bool) *bool {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func (r SearchResult) Candidates(discoveredAt time.Time) []source.Candidate {
	result := make([]source.Candidate, 0, len(r.Hits))
	for _, hit := range r.Hits {
		result = append(result, source.Candidate{
			Repository:   hit.Repository,
			Source:       "github_search",
			Profile:      hit.Profile,
			DiscoveredAt: discoveredAt,
			Metadata: map[string]string{
				"query":            hit.Query,
				"query_rank":       strconv.Itoa(hit.QueryRank),
				"matched_profiles": strings.Join(hit.MatchedProfiles, ","),
			},
		})
	}
	return result
}

// MergeSearchResults deduplicates repository IDs across profiles while retaining
// every matching query. Profile precedence follows input order.
func MergeSearchResults(results ...SearchResult) []SearchHit {
	merged := make([]SearchHit, 0)
	index := make(map[int64]int)
	for _, result := range results {
		for _, hit := range result.Hits {
			position, exists := index[hit.Repository.ID]
			if !exists {
				hit.MatchedQueries = append([]string(nil), hit.MatchedQueries...)
				hit.MatchedProfiles = append([]string(nil), hit.MatchedProfiles...)
				if len(hit.MatchedProfiles) == 0 && hit.Profile != "" {
					hit.MatchedProfiles = []string{hit.Profile}
				}
				index[hit.Repository.ID] = len(merged)
				merged = append(merged, hit)
				continue
			}
			for _, query := range hit.MatchedQueries {
				merged[position].MatchedQueries = appendUniqueString(merged[position].MatchedQueries, query)
			}
			profiles := hit.MatchedProfiles
			if len(profiles) == 0 && hit.Profile != "" {
				profiles = []string{hit.Profile}
			}
			for _, profile := range profiles {
				merged[position].MatchedProfiles = appendUniqueString(merged[position].MatchedProfiles, profile)
			}
		}
	}
	return merged
}

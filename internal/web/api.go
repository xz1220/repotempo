package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xz1220/repotempo/internal/domain"
	"github.com/xz1220/repotempo/internal/service/agentaccess"
)

// Deliberate DTOs prevent internal notes, profiles, operational errors, user
// identifiers and newly added web-only fields leaking through API serialization.
type apiRepositoryDTO struct {
	ID                int64           `json:"id"`
	FullName          string          `json:"full_name"`
	HTMLURL           string          `json:"html_url"`
	Description       string          `json:"description"`
	PrimaryLanguage   string          `json:"primary_language"`
	Stars             *int64          `json:"stars"`
	StarDelta         *int64          `json:"star_delta"`
	GrowthRate        *float64        `json:"growth_rate"`
	Rank              *int64          `json:"rank"`
	RankChange        *int64          `json:"rank_change"`
	Tags              []string        `json:"tags"`
	Topics            []string        `json:"topics"`
	SummaryZH         string          `json:"summary_zh,omitempty"`
	KeyPoints         []string        `json:"key_points,omitempty"`
	IsNew             bool            `json:"is_new"`
	IsStale           bool            `json:"is_stale"`
	IsFocus           *bool           `json:"is_focus,omitempty"`
	LastObservedAt    *time.Time      `json:"last_observed_at,omitempty"`
	LastObservedStars *int64          `json:"last_observed_stars"`
	Analysis          *apiAnalysisDTO `json:"analysis,omitempty"`
}

type apiAnalysisDTO struct {
	SummaryZH      string    `json:"summary_zh"`
	KeyPoints      []string  `json:"key_points"`
	UseCases       []string  `json:"use_cases"`
	TechnicalNotes string    `json:"technical_notes"`
	Source         string    `json:"source"`
	Model          string    `json:"model"`
	AnalyzedAt     time.Time `json:"analyzed_at"`
}
type apiRepositoryList struct {
	Items        []apiRepositoryDTO `json:"items"`
	Total        int                `json:"total"`
	Page         int                `json:"page"`
	Size         int                `json:"size"`
	HasMore      bool               `json:"has_more"`
	AsOf         string             `json:"as_of"`
	BaselineDate string             `json:"baseline_date"`
}
type apiSnapshotDTO struct {
	Date  string `json:"date"`
	Stars *int64 `json:"stars"`
	Rank  *int64 `json:"rank"`
}
type apiRepositoryDetail struct {
	Repository apiRepositoryDTO `json:"repository"`
	AsOf       string           `json:"as_of"`
	History    []apiSnapshotDTO `json:"history"`
}

func repositoryAPIValue(value RepositoryMetric, watch bool) apiRepositoryDTO {
	result := apiRepositoryDTO{ID: value.ID, FullName: value.FullName, HTMLURL: githubURL(value.HTMLURL), Description: value.Description, PrimaryLanguage: value.PrimaryLanguage, Stars: value.CurrentStars, StarDelta: value.StarDelta, GrowthRate: value.GrowthRate, Rank: value.CurrentRank, RankChange: value.RankChange, Tags: append([]string{}, value.Tags...), Topics: []string{}, IsNew: value.IsNew, IsStale: value.IsStale}
	for _, topic := range value.Topics {
		result.Topics = append(result.Topics, topic.Slug)
	}
	if value.Analysis != nil {
		result.SummaryZH = value.Analysis.SummaryZH
		result.KeyPoints = value.Analysis.KeyPoints
		a := value.Analysis
		result.Analysis = &apiAnalysisDTO{SummaryZH: a.SummaryZH, KeyPoints: a.KeyPoints, UseCases: a.UseCases, TechnicalNotes: a.TechnicalNotes, Source: a.Source, Model: a.Model, AnalyzedAt: a.AnalyzedAt}
	}
	result.LastObservedAt = value.LastObservedAt
	result.LastObservedStars = value.LastObservedStars
	if watch {
		focus := value.IsFocus
		result.IsFocus = &focus
	}
	return result
}
func apiDate(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2006-01-02")
}

func apiJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func apiError(w http.ResponseWriter, status int, code string) {
	apiJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}

// A cookie never authenticates an API call. Every accepted key supplies a
// non-administrator principal, including keys issued to site administrators.
func (h *Handler) authenticateAgent(w http.ResponseWriter, r *http.Request) (*http.Request, domain.AgentKey, []byte, bool) {
	if h.agentAccess == nil {
		apiError(w, http.StatusServiceUnavailable, "agent_access_unavailable")
		return r, domain.AgentKey{}, nil, false
	}
	if origin := r.Header.Get("Origin"); origin != "" && !watchSameOrigin(r) {
		apiError(w, http.StatusForbidden, "invalid_origin")
		return r, domain.AgentKey{}, nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if err != nil {
		apiError(w, http.StatusRequestEntityTooLarge, "body_too_large")
		return r, domain.AgentKey{}, nil, false
	}
	if r.Method == http.MethodGet && len(body) > 0 {
		apiError(w, http.StatusBadRequest, "unexpected_body")
		return r, domain.AgentKey{}, nil, false
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	key, err := h.agentAccess.Authenticate(ctx, r, body)
	cancel()
	if err != nil {
		status, code := http.StatusUnauthorized, "invalid_credentials"
		if errors.Is(err, agentaccess.ErrUnavailable) {
			status, code = http.StatusServiceUnavailable, "agent_access_unavailable"
		}
		if errors.Is(err, domain.ErrAgentRateLimited) {
			status, code = http.StatusTooManyRequests, "rate_limited"
			w.Header().Set("Retry-After", "60")
		}
		apiError(w, status, code)
		return r, domain.AgentKey{}, nil, false
	}
	r = r.WithContext(domain.WithPrincipal(r.Context(), domain.Principal{UserID: key.UserID, Login: key.Login, Admin: false}))
	return r, key, body, true
}
func (h *Handler) apiRepositories(w http.ResponseWriter, r *http.Request) {
	r, key, _, ok := h.authenticateAgent(w, r)
	if !ok {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	result, err := h.agentRepositoryList(r.Context(), values, key)
	if err != nil {
		agentQueryError(w, err)
		return
	}
	apiJSON(w, http.StatusOK, result)
}
func (h *Handler) apiRepository(w http.ResponseWriter, r *http.Request) {
	r, key, _, ok := h.authenticateAgent(w, r)
	if !ok {
		return
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || len(values) > 1 || len(values["date"]) > 1 || (len(values) == 1 && !values.Has("date")) {
		apiError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	result, err := h.agentRepositoryDetail(r.Context(), r.PathValue("id"), values.Get("date"), key)
	if err != nil {
		agentQueryError(w, err)
		return
	}
	apiJSON(w, http.StatusOK, result)
}
func (h *Handler) apiMe(w http.ResponseWriter, r *http.Request) {
	_, key, _, ok := h.authenticateAgent(w, r)
	if !ok {
		return
	}
	if r.URL.RawQuery != "" {
		apiError(w, http.StatusBadRequest, "invalid_query")
		return
	}
	apiJSON(w, http.StatusOK, agentAccountValue(key))
}
func agentAccountValue(key domain.AgentKey) any {
	return map[string]any{"user": map[string]any{"github_user_id": key.UserID, "login": key.Login}, "key": map[string]any{"id": key.ID, "name": key.Name, "scopes": key.Scopes, "expires_at": key.ExpiresAt}}
}

func (h *Handler) agentRepositoryList(ctx context.Context, values url.Values, key domain.AgentKey) (apiRepositoryList, error) {
	filter, page, err := h.agentRepositoryFilter(values)
	if err != nil {
		return apiRepositoryList{}, err
	}
	if (filter.OnlyFocus && !agentaccess.HasScope(key, agentaccess.WatchlistRead)) || (!filter.OnlyFocus && !agentaccess.HasScope(key, agentaccess.RepositoriesRead)) {
		return apiRepositoryList{}, agentaccess.ErrForbidden
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := h.queryer.ListRepositoryTrends(ctx, filter)
	if err != nil {
		return apiRepositoryList{}, err
	}
	result := apiRepositoryList{Items: []apiRepositoryDTO{}, Total: data.Total, Page: page, Size: filter.Limit, HasMore: data.HasMore, AsOf: apiDate(data.Coverage.AsOfDate), BaselineDate: apiDate(data.Coverage.BaselineDate)}
	for _, item := range data.Items {
		result.Items = append(result.Items, repositoryAPIValue(item, agentaccess.HasScope(key, agentaccess.WatchlistRead)))
	}
	return result, nil
}

func (h *Handler) agentRepositoryDetail(ctx context.Context, id, rawDate string, key domain.AgentKey) (apiRepositoryDetail, error) {
	if !agentaccess.HasScope(key, agentaccess.RepositoriesRead) {
		return apiRepositoryDetail{}, agentaccess.ErrForbidden
	}
	parsedID, ok := exactPositiveID([]string{id})
	if !ok {
		return apiRepositoryDetail{}, ErrInvalid
	}
	asOf := parseDateParameter(rawDate, h.location)
	if rawDate != "" && (asOf.IsZero() || apiDate(asOf) != rawDate) {
		return apiRepositoryDetail{}, ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	data, err := h.queryer.GetRepositoryDetail(ctx, parsedID, asOf)
	if err != nil {
		return apiRepositoryDetail{}, err
	}
	item := data.Repository
	if item.Analysis == nil {
		item.Analysis = data.Analysis
	}
	result := apiRepositoryDetail{Repository: repositoryAPIValue(item, agentaccess.HasScope(key, agentaccess.WatchlistRead)), AsOf: apiDate(data.AsOf), History: []apiSnapshotDTO{}}
	for _, point := range data.History {
		result.History = append(result.History, apiSnapshotDTO{Date: apiDate(point.Date), Stars: point.Stars, Rank: point.OSSRank})
	}
	return result, nil
}

func (h *Handler) agentRepositoryFilter(values url.Values) (RepositoryQuery, int, error) {
	allowed := []string{"q", "topic", "tag", "source", "status", "date", "period", "sort", "view", "new", "focus", "size", "page"}
	for name, entries := range values {
		if !slices.Contains(allowed, name) || len(entries) != 1 || !utf8.ValidString(entries[0]) || utf8.RuneCountInString(entries[0]) > 200 || strings.ContainsFunc(entries[0], unicode.IsControl) {
			return RepositoryQuery{}, 0, ErrInvalid
		}
	}
	for name, allowedValues := range map[string][]string{
		"period": {"1d", "7d", "30d"}, "sort": {"stars", "delta", "velocity", "rank_change", "growth_rate", "low_growth", "slowdown", "newest", "name"}, "view": {"all", "daily", "focus"}, "new": {"0", "1"}, "focus": {"0", "1"}, "size": {"6", "12", "20"},
		"source": {"github_trending", "github_search", "ossinsight", "legacy", "manual"}, "status": {"active", "paused", "stopped"},
	} {
		if values.Has(name) && !slices.Contains(allowedValues, values.Get(name)) {
			return RepositoryQuery{}, 0, ErrInvalid
		}
	}
	if utf8.RuneCountInString(values.Get("tag")) > 80 {
		return RepositoryQuery{}, 0, ErrInvalid
	}
	page := 1
	if values.Has("page") {
		value, ok := exactPositiveID(values["page"])
		if !ok || value > 100000 {
			return RepositoryQuery{}, 0, ErrInvalid
		}
		page = int(value)
	}
	size := 20
	if values.Has("size") {
		size, _ = strconv.Atoi(values.Get("size"))
	}
	date := parseDateParameter(values.Get("date"), h.location)
	if values.Has("date") && (date.IsZero() || apiDate(date) != values.Get("date")) {
		return RepositoryQuery{}, 0, ErrInvalid
	}
	if values.Get("view") == "focus" && values.Get("focus") == "0" || values.Get("view") == "daily" && values.Get("new") == "0" {
		return RepositoryQuery{}, 0, ErrInvalid
	}
	filter := RepositoryQuery{AsOf: date, WindowDays: 1, Search: cleanSearch(values.Get("q")), TopicSlug: strings.TrimSpace(values.Get("topic")), Tag: strings.TrimSpace(values.Get("tag")), Source: values.Get("source"), MonitoringStatus: values.Get("status"), Sort: "stars", OnlyNew: values.Get("new") == "1" || values.Get("view") == "daily", OnlyFocus: values.Get("focus") == "1" || values.Get("view") == "focus", Limit: size, Offset: repositoryPageOffset(page, size)}
	if values.Has("period") {
		filter.WindowDays = normalizeRepositoryPeriod(values.Get("period"))
	}
	if values.Has("sort") {
		filter.Sort = values.Get("sort")
	}
	if filter.OnlyNew && filter.AsOf.IsZero() {
		filter.AsOf = h.libraryToday()
	}
	return filter, page, nil
}
func agentQueryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, agentaccess.ErrForbidden):
		apiError(w, http.StatusForbidden, "scope_denied")
	case errors.Is(err, ErrInvalid):
		apiError(w, http.StatusBadRequest, "invalid_query")
	case errors.Is(err, ErrNotFound):
		apiError(w, http.StatusNotFound, "not_found")
	default:
		apiError(w, http.StatusServiceUnavailable, "data_unavailable")
	}
}

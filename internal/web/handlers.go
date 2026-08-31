package web

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	repositoryPageSize = 50
	runPageSize        = 50
)

type pageMeta struct {
	Title       string
	Description string
	ActiveNav   string
	SiteName    string
	Warnings    []string
	AsOfLabel   string
	Locale      string
	EnglishURL  string
	ChineseURL  string
	TitleKind   string
}

type pageView struct {
	Meta         pageMeta
	Dashboard    DashboardSummary
	Repositories RepositoryPage
	Repository   RepositoryDetail
	Topics       TopicPage
	Topic        TopicDetail
	Discoveries  DiscoverySummary
	Runs         RunsPage
	StarChart    chart
	RankChart    chart
	TopicChart   chart
	GrowthChart  growthBarChart
	TopicRanking topicRankChart
	DiscoveryMix discoveryMixChart
	TrendPeriods []viewOption
	TrendSorts   []viewOption
	Pagination   pagination
	ErrorStatus  int
	ErrorTitle   string
	ErrorMessage string
}

type viewOption struct {
	Label  string
	URL    string
	Active bool
}

type pagination struct {
	Start       int
	End         int
	Total       int
	PreviousURL string
	NextURL     string
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	h.repositoryIndex(w, r, "/")
}

func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	h.repositoryIndex(w, r, "/repositories")
}

func (h *Handler) repositoryIndex(w http.ResponseWriter, r *http.Request, path string) {
	localized := h.localizerFor(r)
	rawDate := strings.TrimSpace(r.URL.Query().Get("date"))
	asOf := parseDateParameter(rawDate, h.location)
	if rawDate != "" && asOf.IsZero() {
		h.badRequest(w, r, localized.Text("error.invalid_date.title"), localized.Text("error.invalid_date.message"))
		return
	}
	filter := RepositoryQuery{
		AsOf:             asOf,
		WindowDays:       normalizeRepositoryPeriod(r.URL.Query().Get("period")),
		Search:           cleanSearch(r.URL.Query().Get("q")),
		TopicSlug:        strings.TrimSpace(r.URL.Query().Get("topic")),
		Source:           strings.TrimSpace(r.URL.Query().Get("source")),
		MonitoringStatus: strings.TrimSpace(r.URL.Query().Get("status")),
		Sort:             normalizeRepositorySort(r.URL.Query().Get("sort")),
		OnlyNew:          r.URL.Query().Get("new") == "1",
		Limit:            repositoryPageSize,
		AfterID:          parseTrendCursor(r.URL.Query().Get("cursor")),
	}
	data, err := h.queryer.ListRepositoryTrends(r.Context(), filter)
	if err != nil {
		if errors.Is(err, ErrInvalid) && r.URL.Query().Get("cursor") != "" {
			values := cloneValues(r.URL.Query())
			values.Del("cursor")
			http.Redirect(w, r, queryPath(path, values), http.StatusFound)
			return
		}
		h.serverError(w, r, err)
		return
	}
	data.Filter = filter
	data.Filter.AsOf = data.Coverage.AsOfDate
	data.Path = path
	if data.HasMore && data.NextCursor != "" {
		values := cloneValues(r.URL.Query())
		values.Set("cursor", data.NextCursor)
		data.NextCursor = queryPath(path, values)
	}
	view := pageView{
		Meta:         h.meta(localized, "meta.repositories.title", "meta.repositories.description", "repositories", data.Warnings),
		Repositories: data,
		TrendPeriods: repositoryPeriodOptions(path, r.URL.Query(), filter.WindowDays, localized),
		TrendSorts:   repositorySortOptions(path, r.URL.Query(), filter.Sort, localized),
	}
	view.Meta.AsOfLabel = formatDateLocalized(data.Coverage.AsOfDate, h.location, localized.Text("page.not_available"))
	h.render(w, r, http.StatusOK, "repositories", view)
}

func (h *Handler) repository(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.notFound(w, r)
		return
	}
	data, err := h.queryer.GetRepositoryDetail(r.Context(), id, time.Time{})
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.notFound(w, r)
			return
		}
		h.serverError(w, r, err)
		return
	}
	meta := h.metaText(localized, data.Repository.FullName, localized.Text("meta.repository.description"), "repositories", data.Warnings)
	meta.TitleKind = "repository"
	if !data.AsOf.IsZero() {
		meta.AsOfLabel = formatDateLocalized(data.AsOf, h.location, localized.Text("page.not_available"))
	}
	view := pageView{
		Meta:       meta,
		Repository: data,
		StarChart:  snapshotStarChart(data.History, localized),
		RankChart:  snapshotRankChart(data.History, localized),
	}
	h.render(w, r, http.StatusOK, "repository", view)
}

func (h *Handler) topics(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	data, err := h.queryer.ListTopicMetrics(r.Context(), time.Time{})
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	period := normalizeTopicPeriod(r.URL.Query().Get("period"))
	ranking := makeTopicRankChart(data.Items, period, localized)
	for _, value := range []string{"1d", "7d", "30d"} {
		ranking.Options = append(ranking.Options, topicPeriodOption{
			Value:  value,
			Label:  localized.Text("period." + value),
			URL:    topicPeriodURL(r.URL.Query(), value),
			Active: value == period,
		})
	}
	view := pageView{
		Meta:         h.meta(localized, "meta.topics.title", "meta.topics.description", "topics", data.Warnings),
		Topics:       data,
		TopicRanking: ranking,
	}
	if !data.AsOf.IsZero() {
		view.Meta.AsOfLabel = formatDateLocalized(data.AsOf, h.location, localized.Text("page.not_available"))
	}
	h.render(w, r, http.StatusOK, "topics", view)
}

func (h *Handler) topic(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	slug := strings.TrimSpace(r.PathValue("slug"))
	if !validSlug(slug) {
		h.notFound(w, r)
		return
	}
	excludeLeader := r.URL.Query().Get("exclude_leader") == "true"
	data, err := h.queryer.GetTopicDetail(r.Context(), slug, time.Time{}, excludeLeader)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.notFound(w, r)
			return
		}
		h.serverError(w, r, err)
		return
	}
	meta := h.metaText(localized, data.Topic.Name, localized.Text("meta.topic.description"), "topics", data.Warnings)
	meta.TitleKind = "topic"
	if !data.AsOf.IsZero() {
		meta.AsOfLabel = formatDateLocalized(data.AsOf, h.location, localized.Text("page.not_available"))
	}
	view := pageView{
		Meta:       meta,
		Topic:      data,
		TopicChart: trendChart(data.History, localized.Text("chart.topic_history"), localized.Text("chart.topic_history_help")),
	}
	h.render(w, r, http.StatusOK, "topic", view)
}

func (h *Handler) discoveries(w http.ResponseWriter, r *http.Request) {
	values := cloneValues(r.URL.Query())
	values.Set("new", "1")
	values.Del("cursor")
	http.Redirect(w, r, queryPath("/", values), http.StatusFound)
}

func (h *Handler) runs(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	offset := parseOffset(r.URL.Query().Get("offset"))
	data, err := h.queryer.ListJobRuns(r.Context(), runPageSize, offset)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data.Limit = runPageSize
	data.Offset = offset
	h.render(w, r, http.StatusOK, "runs", pageView{
		Meta:       h.meta(localized, "meta.runs.title", "meta.runs.description", "runs", data.Warnings),
		Runs:       data,
		Pagination: basicPagination("/runs", offset, runPageSize, data.Total),
	})
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if err := h.queryer.Ready(r.Context()); err != nil {
		h.logger.WarnContext(r.Context(), "web readiness check failed", "error", err)
		http.Error(w, "not ready", http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	h.render(w, r, http.StatusNotFound, "error", pageView{
		Meta:         h.meta(localized, "error.not_found.title", "error.not_found.description", "", nil),
		ErrorStatus:  http.StatusNotFound,
		ErrorTitle:   localized.Text("error.not_found.title"),
		ErrorMessage: localized.Text("error.not_found.message"),
	})
}

func (h *Handler) badRequest(w http.ResponseWriter, r *http.Request, title, message string) {
	localized := h.localizerFor(r)
	h.render(w, r, http.StatusBadRequest, "error", pageView{
		Meta:         h.metaText(localized, title, message, "", nil),
		ErrorStatus:  http.StatusBadRequest,
		ErrorTitle:   title,
		ErrorMessage: message,
	})
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, err error) {
	localized := h.localizerFor(r)
	h.logger.ErrorContext(r.Context(), "web query failed", "path", r.URL.Path, "error", err)
	h.render(w, r, http.StatusInternalServerError, "error", pageView{
		Meta:         h.meta(localized, "error.unavailable.title", "error.unavailable.description", "", nil),
		ErrorStatus:  http.StatusInternalServerError,
		ErrorTitle:   localized.Text("error.unavailable.title"),
		ErrorMessage: localized.Text("error.unavailable.message"),
	})
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, status int, name string, data pageView) {
	locale := h.localeFor(r)
	templates, ok := h.templates[locale]
	if !ok {
		templates = h.templates[localeEnglish]
	}
	tmpl, ok := templates[name]
	if !ok {
		http.Error(w, "template unavailable", http.StatusInternalServerError)
		return
	}
	data.Meta.Locale = locale
	data.Meta.EnglishURL = languageURL(r, localeEnglish)
	data.Meta.ChineseURL = languageURL(r, localeChinese)
	var output bytes.Buffer
	if err := tmpl.ExecuteTemplate(&output, "base", data); err != nil {
		h.logger.ErrorContext(r.Context(), "web template failed", "template", name, "error", err)
		http.Error(w, "template unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Language", locale)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = output.WriteTo(w)
}

func (h *Handler) meta(localized localizer, titleKey, descriptionKey, active string, warnings []string) pageMeta {
	return h.metaText(localized, localized.Text(titleKey), localized.Text(descriptionKey), active, warnings)
}

func (h *Handler) metaText(localized localizer, title, description, active string, warnings []string) pageMeta {
	localizedWarnings := make([]string, len(warnings))
	for index, warning := range warnings {
		localizedWarnings[index] = localized.WarningText(warning)
	}
	return pageMeta{
		Title:       title,
		Description: description,
		ActiveNav:   active,
		SiteName:    h.siteName,
		Warnings:    localizedWarnings,
		AsOfLabel:   formatDateTime(h.now(), h.location),
	}
}

func (h *Handler) localeFor(r *http.Request) string {
	if locale, ok := requestedLocale(r.URL.Query().Get("lang")); ok {
		return locale
	}
	cookie, err := r.Cookie(localeCookieName)
	if err == nil {
		if locale, ok := requestedLocale(cookie.Value); ok {
			return locale
		}
	}
	return h.locale
}

func (h *Handler) localizerFor(r *http.Request) localizer {
	return newLocalizer(h.localeFor(r))
}

func languageURL(r *http.Request, locale string) string {
	values := r.URL.Query()
	values.Set("lang", locale)
	return queryPath(r.URL.Path, values)
}

func (h *Handler) asOf() time.Time {
	return h.now().In(h.location)
}

func cleanSearch(raw string) string {
	value := strings.Join(strings.Fields(raw), " ")
	if utf8.RuneCountInString(value) <= 120 {
		return value
	}
	runes := []rune(value)
	return string(runes[:120])
}

func parseOffset(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func parseDateParameter(raw string, location *time.Location) time.Time {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, location)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func normalizeRepositoryPeriod(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1d":
		return 1
	case "30d":
		return 30
	default:
		return 7
	}
}

func normalizeRepositorySort(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "rank_change", "stars", "delta", "growth_rate":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "velocity"
	}
}

func parseTrendCursor(raw string) *int64 {
	value, err := strconv.ParseInt(strings.TrimSpace(raw), 36, 64)
	if err != nil || value <= 0 {
		return nil
	}
	return &value
}

func cloneValues(values url.Values) url.Values {
	result := make(url.Values, len(values))
	for key, items := range values {
		result[key] = append([]string(nil), items...)
	}
	return result
}

func repositoryOptionURL(path string, values url.Values, key, value string) string {
	result := cloneValues(values)
	result.Set(key, value)
	result.Del("cursor")
	return queryPath(path, result)
}

func repositoryPeriodOptions(path string, values url.Values, active int, localized localizer) []viewOption {
	options := make([]viewOption, 0, 3)
	for _, value := range []struct {
		Days  int
		Query string
	}{
		{Days: 1, Query: "1d"},
		{Days: 7, Query: "7d"},
		{Days: 30, Query: "30d"},
	} {
		options = append(options, viewOption{
			Label:  localized.Text("period." + value.Query),
			URL:    repositoryOptionURL(path, values, "period", value.Query),
			Active: active == value.Days,
		})
	}
	return options
}

func repositorySortOptions(path string, values url.Values, active string, localized localizer) []viewOption {
	options := make([]viewOption, 0, 4)
	for _, value := range []string{"velocity", "growth_rate", "rank_change", "stars"} {
		options = append(options, viewOption{
			Label:  localized.Text("repositories.sort_" + value),
			URL:    repositoryOptionURL(path, values, "sort", value),
			Active: active == value,
		})
	}
	return options
}

func normalizeTopicPeriod(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1d":
		return "1d"
	case "30d":
		return "30d"
	default:
		return "7d"
	}
}

func topicPeriodURL(values url.Values, period string) string {
	copyValues := make(url.Values, len(values)+1)
	for key, items := range values {
		copyValues[key] = append([]string(nil), items...)
	}
	copyValues.Set("period", period)
	return queryPath("/topics", copyValues)
}

func validSlug(value string) bool {
	if len(value) == 0 || len(value) > 80 {
		return false
	}
	for index, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			continue
		}
		if char == '-' && index > 0 && index < len(value)-1 {
			continue
		}
		return false
	}
	return true
}

func repositoryPagination(values url.Values, offset, limit, total int) pagination {
	page := make(url.Values, len(values))
	for key, items := range values {
		page[key] = append([]string(nil), items...)
	}
	page.Del("offset")
	path := func(next int) string {
		copyValues := make(url.Values, len(page)+1)
		for key, items := range page {
			copyValues[key] = append([]string(nil), items...)
		}
		if next > 0 {
			copyValues.Set("offset", strconv.Itoa(next))
		}
		encoded := copyValues.Encode()
		if encoded == "" {
			return "/repositories"
		}
		return "/repositories?" + encoded
	}
	result := pagination{Start: min(offset+1, total), End: min(offset+limit, total), Total: total}
	if offset > 0 {
		result.PreviousURL = path(max(0, offset-limit))
	}
	if offset+limit < total {
		result.NextURL = path(offset + limit)
	}
	return result
}

func basicPagination(path string, offset, limit, total int) pagination {
	result := pagination{Start: min(offset+1, total), End: min(offset+limit, total), Total: total}
	if offset > 0 {
		result.PreviousURL = fmt.Sprintf("%s?offset=%d", path, max(0, offset-limit))
	}
	if offset+limit < total {
		result.NextURL = fmt.Sprintf("%s?offset=%d", path, offset+limit)
	}
	return result
}

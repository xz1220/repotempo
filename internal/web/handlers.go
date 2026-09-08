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
	"unicode"
	"unicode/utf8"
)

const (
	repositoryPageSize = 20
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
	Stale       bool
}

type pageView struct {
	Meta              pageMeta
	Dashboard         DashboardSummary
	Repositories      RepositoryPage
	Repository        RepositoryDetail
	Topics            TopicPage
	Topic             TopicDetail
	Discoveries       DiscoverySummary
	Runs              RunsPage
	StarChart         chart
	RankChart         chart
	TopicChart        chart
	GrowthChart       growthBarChart
	TopicRanking      topicRankChart
	DiscoveryMix      discoveryMixChart
	TrendPeriods      []viewOption
	TrendSorts        []viewOption
	LibraryViews      []viewOption
	AllProjectsURL    string
	NewProjectsURL    string
	Pagination        pagination
	ErrorStatus       int
	ErrorTitle        string
	ErrorMessage      string
	Radar             RadarOverview
	RadarChart        chart
	Boards            []leaderboard
	Categories        []categoryLink
	CurrentPath       string
	ChartChange       *float64
	ChartCohort       int
	DirectionSegments []directionSegment
	CategoryCards     []categoryCard
	RepositoryTags    map[int64]cardTagLinks
	DetailTags        cardTagLinks
	ActiveFilters     []viewOption
	SuggestedTags     []viewOption

	ListURL              string
	RepositoryDetailURLs map[int64]string
	LibraryReturnURL     string
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
	h.radarHome(w, r)
}

func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	h.repositoryIndex(w, r, "/repositories")
}

func (h *Handler) repositoryIndex(w http.ResponseWriter, r *http.Request, path string) {
	localized := h.localizerFor(r)
	tag := strings.TrimSpace(r.URL.Query().Get("tag"))
	if !utf8.ValidString(tag) || utf8.RuneCountInString(tag) > 80 || strings.ContainsFunc(tag, unicode.IsControl) {
		h.badRequest(w, r, localized.Text("tags.invalid"), localized.Text("tags.invalid_help"))
		return
	}
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
		Tag:              tag,
		Source:           strings.TrimSpace(r.URL.Query().Get("source")),
		MonitoringStatus: strings.TrimSpace(r.URL.Query().Get("status")),
		Sort:             normalizeRepositorySort(r.URL.Query().Get("sort")),
		OnlyNew:          r.URL.Query().Get("new") == "1",
		OnlyFocus:        r.URL.Query().Get("focus") == "1",
		Limit:            repositoryPageSize,
		AfterID:          parseTrendCursor(r.URL.Query().Get("cursor")),
	}
	h.applyLibraryDefaults(r.URL.Query(), &filter)
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
	if data.Coverage.AsOfDate.IsZero() && !filter.AsOf.IsZero() {
		data.Coverage.AsOfDate = filter.AsOf
		data.Coverage.BaselineDate = filter.AsOf.AddDate(0, 0, -filter.WindowDays)
	}
	data.Filter = filter
	data.Filter.AsOf = data.Coverage.AsOfDate
	data.Path = path
	data.Sources = []string{"github_trending", "github_search", "ossinsight", "legacy", "manual"}
	firstPageValues := cloneValues(r.URL.Query())
	firstPageValues.Del("cursor")
	firstPageValues.Set("date", data.Coverage.AsOfDate.Format("2006-01-02"))
	if filter.OnlyNew {
		// Bare project-library URLs default to daily additions. Preserve that
		// choice when links gain explicit date/category/pagination parameters.
		firstPageValues.Set("new", "1")
	}
	data.FirstPageURL = queryPath(path, firstPageValues)
	if data.HasMore && data.NextCursor != "" {
		values := cloneValues(firstPageValues)
		values.Set("cursor", data.NextCursor)
		values.Set("date", data.Coverage.AsOfDate.Format("2006-01-02"))
		data.NextCursor = queryPath(path, values)
	}
	view := pageView{
		Meta:           h.meta(localized, "meta.repositories.title", "meta.repositories.description", "repositories", data.Warnings),
		Repositories:   data,
		TrendPeriods:   repositoryPeriodOptions(path, firstPageValues, filter.WindowDays, localized),
		TrendSorts:     repositorySortOptions(path, firstPageValues, filter.Sort, localized),
		LibraryViews:   h.libraryViewOptions(path, r.URL.Query(), data.Filter, localized),
		AllProjectsURL: queryPath(path, url.Values{"view": {"all"}, "new": {"0"}, "lang": {h.localeFor(r)}}),
	}
	view.Meta.AsOfLabel = formatDateLocalized(data.Coverage.AsOfDate, h.location, localized.Text("page.not_available"))
	view.Meta.Stale = rawDate == "" && h.isStale(data.Coverage.AsOfDate)
	view.CurrentPath = path
	view.Categories = categoryLinks(path, firstPageValues)
	view.RepositoryTags = make(map[int64]cardTagLinks, len(data.Items))
	view.ListURL = h.canonicalLibraryURL(r.URL.Query(), data.Filter, h.localeFor(r))
	view.RepositoryDetailURLs = make(map[int64]string, len(data.Items))
	view.SuggestedTags = suggestedTagLinks(data.Tags, firstPageValues, localized)
	if filter.OnlyNew && filter.Tag != "" {
		view.AllProjectsURL = view.LibraryViews[1].URL
	}
	for _, item := range data.Items {
		view.RepositoryTags[item.ID] = makeCardTagLinks(item.Tags, item.Topics, firstPageValues, localized)
		view.RepositoryDetailURLs[item.ID] = repositoryDetailURL(item.ID, view.ListURL, data.Filter.AsOf.Format("2006-01-02"), h.localeFor(r))
	}
	for _, active := range []struct{ key, value, label string }{
		{"tag", filter.Tag, localized.Textf("tags.active", filter.Tag)},
		{"topic", filter.TopicSlug, localized.Textf("tags.legacy_topic", localized.TopicName(filter.TopicSlug, filter.TopicSlug))},
		{"source", filter.Source, localized.Textf("tags.legacy_source", localized.SourceLabel(filter.Source))},
	} {
		if active.value != "" {
			values := cloneValues(firstPageValues)
			values.Del(active.key)
			view.ActiveFilters = append(view.ActiveFilters, viewOption{Label: active.label, URL: queryPath(path, values)})
		}
	}
	h.render(w, r, http.StatusOK, "repositories", view)
}

func (h *Handler) libraryToday() time.Time {
	// Derive the calendar date in Shanghai even when a test or embedding host
	// uses a different display timezone. Never substitute the last populated day.
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	return parseDateParameter(h.now().In(shanghai).Format("2006-01-02"), h.location)
}

func (h *Handler) applyLibraryDefaults(values url.Values, filter *RepositoryQuery) {
	if !values.Has("focus") && values.Get("view") == "focus" {
		filter.OnlyFocus = true
	}
	if !values.Has("new") {
		switch values.Get("view") {
		case "daily":
			filter.OnlyNew = true
		case "all", "focus":
			filter.OnlyNew = false
		default:
			// Old shared URLs already express a scope. Preserve their full-library
			// meaning instead of narrowing searches or dashboard leaderboard links.
			explicit := false
			for _, key := range []string{"view", "sort", "period", "date", "topic", "tag", "q", "source", "status", "focus", "cursor"} {
				explicit = explicit || values.Has(key)
			}
			filter.OnlyNew = !explicit
		}
	}
	if filter.OnlyNew {
		if filter.AsOf.IsZero() {
			filter.AsOf = h.libraryToday()
		}
		if !values.Has("period") || strings.TrimSpace(values.Get("period")) == "" {
			filter.WindowDays = 1
		}
		if !values.Has("sort") || strings.TrimSpace(values.Get("sort")) == "" {
			filter.Sort = "stars"
		}
	}
}

func (h *Handler) libraryViewOptions(path string, original url.Values, filter RepositoryQuery, localized localizer) []viewOption {
	dailyDate := h.libraryToday()
	if strings.TrimSpace(original.Get("date")) != "" {
		dailyDate = filter.AsOf
	}
	label := localized.Text("daily.today")
	if dailyDate.Format("2006-01-02") != h.libraryToday().Format("2006-01-02") {
		label = localized.Textf("daily.on_date", dailyDate.Format("2006-01-02"))
	}
	link := func(view string) string {
		values := cloneValues(original)
		values.Del("cursor")
		values.Set("view", view)
		values.Del("focus")
		values.Set("new", "0")
		if !filter.AsOf.IsZero() {
			values.Set("date", filter.AsOf.Format("2006-01-02"))
		}
		if view == "daily" {
			values.Set("new", "1")
			values.Set("date", dailyDate.Format("2006-01-02"))
		} else if view == "focus" {
			values.Set("focus", "1")
		}
		return queryPath(path, values)
	}
	return []viewOption{
		{Label: label, URL: link("daily"), Active: filter.OnlyNew && !filter.OnlyFocus},
		{Label: localized.Text("ui.all_library"), URL: link("all"), Active: !filter.OnlyNew && !filter.OnlyFocus},
		{Label: localized.Text("ui.my_watchlist"), URL: link("focus"), Active: filter.OnlyFocus},
	}
}

func (h *Handler) repository(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.notFound(w, r)
		return
	}
	rawDate := strings.TrimSpace(r.URL.Query().Get("date"))
	asOf := parseDateParameter(rawDate, h.location)
	if rawDate != "" && asOf.IsZero() {
		h.badRequest(w, r, localized.Text("error.invalid_date.title"), localized.Text("error.invalid_date.message"))
		return
	}
	data, err := h.queryer.GetRepositoryDetail(r.Context(), id, asOf)
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
		Meta:             meta,
		Repository:       data,
		StarChart:        snapshotStarChart(data.History, localized),
		RankChart:        snapshotRankChart(data.History, localized),
		DetailTags:       makeCardTagLinks(data.Repository.Tags, data.Repository.Topics, url.Values{"view": {"all"}, "date": {data.AsOf.Format("2006-01-02")}, "lang": {h.localeFor(r)}}, localized),
		LibraryReturnURL: h.libraryReturnURL(r, id),
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
		Meta:          h.meta(localized, "meta.topics.title", "meta.topics.description", "repositories", data.Warnings),
		Topics:        data,
		TopicRanking:  ranking,
		CategoryCards: categoryCards(data.Items, localized),
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
	meta := h.metaText(localized, localized.TopicName(data.Topic.Slug, data.Topic.Name), localized.Text("meta.topic.description"), "repositories", data.Warnings)
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
	values := r.URL.Query()
	values.Set("new", "1")
	values.Del("cursor")
	if values.Get("period") == "" {
		values.Set("period", "1d")
	}
	if values.Get("sort") == "" {
		values.Set("sort", "stars")
	}
	http.Redirect(w, r, queryPath("/repositories", values), http.StatusFound)
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
	if data.LibraryReturnURL != "" {
		data.Meta.EnglishURL = repositoryLanguageURL(r, localeEnglish, data.LibraryReturnURL)
		data.Meta.ChineseURL = repositoryLanguageURL(r, localeChinese, data.LibraryReturnURL)
	}
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
	case "rank_change", "stars", "delta", "growth_rate", "low_growth", "slowdown", "newest":
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

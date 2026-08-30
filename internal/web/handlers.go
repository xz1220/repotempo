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
	Pagination   pagination
	ErrorStatus  int
	ErrorTitle   string
	ErrorMessage string
}

type pagination struct {
	Start       int
	End         int
	Total       int
	PreviousURL string
	NextURL     string
}

func (h *Handler) home(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	asOf := h.asOf()
	data, err := h.queryer.DashboardSummary(r.Context(), asOf)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "home", pageView{
		Meta:      h.meta(localized, "meta.overview.title", "meta.overview.description", "overview", data.Warnings),
		Dashboard: data,
	})
}

func (h *Handler) repositories(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	filter := RepositoryQuery{
		AsOf:             h.asOf(),
		Search:           cleanSearch(r.URL.Query().Get("q")),
		TopicSlug:        strings.TrimSpace(r.URL.Query().Get("topic")),
		Source:           strings.TrimSpace(r.URL.Query().Get("source")),
		MonitoringStatus: strings.TrimSpace(r.URL.Query().Get("status")),
		Limit:            repositoryPageSize,
		Offset:           parseOffset(r.URL.Query().Get("offset")),
	}
	data, err := h.queryer.ListRepositoryMetrics(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	data.Filter = filter
	view := pageView{
		Meta:         h.meta(localized, "meta.repositories.title", "meta.repositories.description", "repositories", data.Warnings),
		Repositories: data,
		Pagination:   repositoryPagination(r.URL.Query(), filter.Offset, filter.Limit, data.Total),
	}
	h.render(w, r, http.StatusOK, "repositories", view)
}

func (h *Handler) repository(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		h.notFound(w, r)
		return
	}
	data, err := h.queryer.GetRepositoryDetail(r.Context(), id, h.asOf())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.notFound(w, r)
			return
		}
		h.serverError(w, r, err)
		return
	}
	view := pageView{
		Meta:       h.metaText(localized, data.Repository.FullName, localized.Text("meta.repository.description"), "repositories", data.Warnings),
		Repository: data,
		StarChart:  snapshotStarChart(data.History, localized),
		RankChart:  snapshotRankChart(data.History, localized),
	}
	h.render(w, r, http.StatusOK, "repository", view)
}

func (h *Handler) topics(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	data, err := h.queryer.ListTopicMetrics(r.Context(), h.asOf())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "topics", pageView{
		Meta:   h.meta(localized, "meta.topics.title", "meta.topics.description", "topics", data.Warnings),
		Topics: data,
	})
}

func (h *Handler) topic(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	slug := strings.TrimSpace(r.PathValue("slug"))
	if !validSlug(slug) {
		h.notFound(w, r)
		return
	}
	excludeLeader := r.URL.Query().Get("exclude_leader") == "true"
	data, err := h.queryer.GetTopicDetail(r.Context(), slug, h.asOf(), excludeLeader)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			h.notFound(w, r)
			return
		}
		h.serverError(w, r, err)
		return
	}
	view := pageView{
		Meta:       h.metaText(localized, data.Topic.Name, localized.Text("meta.topic.description"), "topics", data.Warnings),
		Topic:      data,
		TopicChart: trendChart(data.History, localized.Text("chart.topic_history"), localized.Text("chart.topic_history_help")),
	}
	h.render(w, r, http.StatusOK, "topic", view)
}

func (h *Handler) discoveries(w http.ResponseWriter, r *http.Request) {
	localized := h.localizerFor(r)
	data, err := h.queryer.DiscoverySummary(r.Context())
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	h.render(w, r, http.StatusOK, "discoveries", pageView{
		Meta:        h.meta(localized, "meta.discoveries.title", "meta.discoveries.description", "discoveries", data.Warnings),
		Discoveries: data,
	})
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

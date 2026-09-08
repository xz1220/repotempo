package web

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const maxLibraryReturnBytes = 8192

// canonicalLibraryURL records the effective scope, not just the incoming query.
// In particular, a daily view must not silently become tomorrow's additions.
func (h *Handler) canonicalLibraryURL(original url.Values, filter RepositoryQuery, locale string) string {
	values := url.Values{
		"date":   {filter.AsOf.Format("2006-01-02")},
		"period": {strconv.Itoa(filter.WindowDays) + "d"},
		"sort":   {filter.Sort},
		"new":    {"0"},
		"focus":  {"0"},
		"lang":   {locale},
	}
	if filter.AsOf.IsZero() {
		values.Set("date", h.libraryToday().Format("2006-01-02"))
	}
	if filter.OnlyNew {
		values.Set("new", "1")
	}
	if filter.OnlyFocus {
		values.Set("focus", "1")
	}
	view := original.Get("view")
	if view != "daily" && view != "all" && view != "focus" {
		switch {
		case filter.OnlyFocus:
			view = "focus"
		case filter.OnlyNew:
			view = "daily"
		default:
			view = "all"
		}
	}
	values.Set("view", view)
	for key, value := range map[string]string{
		"q": filter.Search, "topic": filter.TopicSlug, "tag": filter.Tag,
		"source": filter.Source, "status": filter.MonitoringStatus,
	} {
		if value != "" {
			values.Set(key, value)
		}
	}
	if filter.AfterID != nil {
		values.Set("cursor", strconv.FormatInt(*filter.AfterID, 36))
	}
	if safe, ok := validatedLibraryReturnURL(queryPath("/repositories", values)); ok {
		return safe
	}
	return "/repositories"
}

func repositoryDetailURL(id int64, listURL, date, locale string) string {
	return queryPath("/repositories/"+strconv.FormatInt(id, 10), url.Values{
		"date": {date}, "lang": {locale},
		"return_to": {listURL + "#project-" + strconv.FormatInt(id, 10)},
	})
}

// Preserve legacy Referer context and the active language through a switch.
func repositoryLanguageURL(r *http.Request, locale, returnURL string) string {
	values := r.URL.Query()
	values.Set("lang", locale)
	if safe, ok := validatedLibraryReturnURL(returnURL); ok {
		parsed, _ := url.Parse(safe)
		listValues := parsed.Query()
		listValues.Set("lang", locale)
		parsed.RawQuery = listValues.Encode()
		values.Set("return_to", parsed.String())
	}
	return queryPath(r.URL.Path, values)
}

// validatedLibraryReturnURL deliberately supports one local destination only.
// Never broaden this to an arbitrary relative URL or decode its path twice.
func validatedLibraryReturnURL(raw string) (string, bool) {
	if raw == "" || len(raw) > maxLibraryReturnBytes || unsafeNavigationText(raw) {
		return "", false
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Opaque != "" ||
		parsed.Path != "/repositories" || parsed.EscapedPath() != "/repositories" {
		return "", false
	}
	if parsed.Fragment != "" {
		id, err := strconv.ParseInt(strings.TrimPrefix(parsed.Fragment, "project-"), 10, 64)
		if err != nil || id <= 0 || parsed.Fragment != "project-"+strconv.FormatInt(id, 10) {
			return "", false
		}
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	if err != nil {
		return "", false
	}
	for key, entries := range values {
		if len(entries) != 1 || unsafeNavigationText(entries[0]) {
			return "", false
		}
		value := entries[0]
		switch key {
		case "date":
			if value != "" && parseDateParameter(value, time.UTC).IsZero() {
				return "", false
			}
		case "period":
			if value != "1d" && value != "7d" && value != "30d" {
				return "", false
			}
		case "sort":
			if value != normalizeRepositorySort(value) {
				return "", false
			}
		case "new", "focus":
			if value != "0" && value != "1" {
				return "", false
			}
		case "view":
			if value != "daily" && value != "all" && value != "focus" {
				return "", false
			}
		case "lang":
			if value != localeEnglish && value != localeChinese {
				return "", false
			}
		case "cursor":
			if parseTrendCursor(value) == nil {
				return "", false
			}
		case "tag", "topic", "source", "status", "q":
			// These are data, never URLs. html/template escapes their display,
			// and Values.Encode keeps them inside their own query value.
		default:
			return "", false
		}
	}
	parsed.RawQuery = values.Encode()
	canonical := parsed.String()
	if len(canonical) > maxLibraryReturnBytes {
		return "", false
	}
	return canonical, true
}

func unsafeNavigationText(value string) bool {
	return !utf8.ValidString(value) || strings.Contains(value, "\\") || strings.ContainsFunc(value, unicode.IsControl)
}

func (h *Handler) libraryReturnURL(r *http.Request, repositoryID int64) string {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return "/repositories"
	}
	if values.Has("return_to") {
		if len(values["return_to"]) == 1 {
			if safe, ok := validatedLibraryReturnURL(values.Get("return_to")); ok {
				return safe
			}
		}
		// An explicitly invalid destination must not be replaced by Referer.
		return "/repositories"
	}
	referrer, err := url.Parse(r.Referer())
	scheme := "http"
	if watchHTTPS(r) {
		scheme = "https"
	}
	if err != nil || referrer.Scheme != scheme || !strings.EqualFold(referrer.Host, r.Host) || referrer.User != nil {
		return "/repositories"
	}
	referrer.Scheme, referrer.Host = "", ""
	referrer.Fragment = "project-" + strconv.FormatInt(repositoryID, 10)
	if safe, ok := validatedLibraryReturnURL(referrer.String()); ok {
		// Old bare-library links meant today's additions. If that old detail
		// carries an explicit date, retain it even when returning after midnight.
		referrerValues := referrer.Query()
		date := parseDateParameter(values.Get("date"), time.UTC)
		if !date.IsZero() && (len(referrerValues) == 0 || (len(referrerValues) == 1 && referrerValues.Has("lang"))) {
			referrerValues.Set("date", date.Format("2006-01-02"))
			referrerValues.Set("new", "1")
			referrerValues.Set("view", "daily")
			referrerValues.Set("period", "1d")
			referrerValues.Set("sort", "stars")
			referrer.RawQuery = referrerValues.Encode()
			return referrer.String()
		}
		return safe
	}
	return "/repositories"
}

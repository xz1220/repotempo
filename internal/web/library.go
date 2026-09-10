package web

import (
	"net/url"
	"strconv"
	"strings"
	"time"
)

func (h *Handler) libraryToday() time.Time {
	// Derive the calendar date in Shanghai even if the display timezone differs.
	shanghai := time.FixedZone("Asia/Shanghai", 8*60*60)
	return parseDateParameter(h.now().In(shanghai).Format("2006-01-02"), h.location)
}

func (h *Handler) applyLibraryDefaults(values url.Values, filter *RepositoryQuery) {
	if !values.Has("focus") && values.Get("view") == "focus" {
		filter.OnlyFocus = true
	}
	if !values.Has("new") {
		switch {
		case filter.OnlyFocus:
			filter.OnlyNew = false
		case values.Get("view") == "daily":
			filter.OnlyNew = true
		case values.Get("view") == "all":
			filter.OnlyNew = false
		default:
			// Existing shared links with no explicit view retain their scope.
			explicit := false
			for _, key := range []string{"view", "sort", "period", "date", "topic", "tag", "q", "source", "status", "focus", "cursor", "page", "size"} {
				explicit = explicit || values.Has(key)
			}
			filter.OnlyNew = !explicit
		}
	}
	if filter.OnlyNew {
		if filter.AsOf.IsZero() {
			filter.AsOf = h.libraryToday()
		}
	}
	if strings.TrimSpace(values.Get("period")) == "" {
		filter.WindowDays = 1
	}
	if strings.TrimSpace(values.Get("sort")) == "" {
		filter.Sort = "stars"
	}
}

func (h *Handler) libraryViewOptions(path string, original url.Values, filter RepositoryQuery, localized localizer) []viewOption {
	link := func(view string) string {
		values := cloneValues(original)
		values.Del("cursor")
		values.Del("page")
		values.Set("view", view)
		values.Set("focus", "0")
		values.Set("new", "0")
		switch view {
		case "daily":
			values.Set("new", "1")
		case "focus":
			values.Set("focus", "1")
		}
		if !filter.AsOf.IsZero() {
			values.Set("date", filter.AsOf.Format("2006-01-02"))
		}
		values.Set("period", strconv.Itoa(filter.WindowDays)+"d")
		values.Set("sort", filter.Sort)
		values.Set("lang", localized.locale)
		return queryPath(path, values)
	}
	return []viewOption{
		{Label: localized.Text("daily.today"), URL: link("daily"), Active: filter.OnlyNew && !filter.OnlyFocus},
		{Label: localized.Text("ui.all_library"), URL: link("all"), Active: !filter.OnlyNew && !filter.OnlyFocus},
		{Label: localized.Text("ui.my_watchlist"), URL: link("focus"), Active: filter.OnlyFocus},
	}
}

func init() {
	messageCatalog[localeChinese]["library.import"] = "导入"
	messageCatalog[localeEnglish]["library.import"] = "Import"
}

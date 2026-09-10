package web

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func libraryExportURL(raw string) string {
	valid, ok := validatedLibraryReturnURL(raw)
	if !ok {
		return "/repositories/export"
	}
	u, _ := url.Parse(valid)
	u.Path = "/repositories/export"
	u.Fragment = ""
	q := u.Query()
	q.Del("cursor")
	q.Del("page")
	q.Del("size")
	u.RawQuery = q.Encode()
	return u.String()
}

// CSV exports the selected server-side scope, bounded independently from the UI
// page size. Private notes and operational metadata are deliberately excluded.
func (h *Handler) exportRepositories(w http.ResponseWriter, r *http.Request) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		apiError(w, 400, "invalid_query")
		return
	}
	values.Del("lang")
	filter, _, err := h.agentRepositoryFilter(values)
	if err != nil {
		apiError(w, 400, "invalid_query")
		return
	}
	filter.Limit = 100
	filter.Offset = 0
	query := h.queryer.ListRepositoryTrends
	if exporter, ok := h.queryer.(interface {
		ExportRepositoryTrends(context.Context, RepositoryQuery) (RepositoryPage, error)
	}); ok {
		query = exporter.ExportRepositoryTrends
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	var output bytes.Buffer
	output.WriteString("\ufeff")
	writer := csv.NewWriter(&output)
	_ = writer.Write([]string{"repository_id", "repository", "description", "observation_date", "stars", "period_gain", "growth_rate", "entered_at", "github_url"})
	for {
		page, err := query(ctx, filter)
		if err != nil {
			h.serverError(w, r, err)
			return
		}
		if page.Total > 10000 {
			apiError(w, http.StatusRequestEntityTooLarge, "narrow_export_filters")
			return
		}
		if filter.AsOf.IsZero() {
			filter.AsOf = page.Coverage.AsOfDate
		}
		for _, item := range page.Items {
			stars, gain, rate := "", "", ""
			if item.CurrentStars != nil {
				stars = strconv.FormatInt(*item.CurrentStars, 10)
			}
			if item.StarDelta != nil {
				gain = strconv.FormatInt(*item.StarDelta, 10)
			}
			if item.GrowthRate != nil {
				rate = strconv.FormatFloat(*item.GrowthRate, 'f', 6, 64)
			}
			_ = writer.Write([]string{strconv.FormatInt(item.ID, 10), safeCSVText(item.FullName), safeCSVText(item.Description), apiDate(page.Coverage.AsOfDate), stars, gain, rate, apiDate(item.FirstSeenAt), projectGitHubURL(item.FullName, item.HTMLURL)})
		}
		filter.Offset += len(page.Items)
		if len(page.Items) == 0 || !page.HasMore || filter.Offset >= page.Total {
			break
		}
		if filter.Offset >= 10000 {
			apiError(w, http.StatusRequestEntityTooLarge, "narrow_export_filters")
			return
		}
	}
	writer.Flush()
	if writer.Error() != nil {
		h.serverError(w, r, writer.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="repotempo-%s.csv"`, apiDate(filter.AsOf)))
	_, _ = w.Write(output.Bytes())
}

func safeCSVText(value string) string {
	trimmed := strings.TrimLeft(value, " \r\n\t")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) || strings.HasPrefix(value, "\t") || strings.HasPrefix(value, "\r") {
		return "'" + value
	}
	return value
}

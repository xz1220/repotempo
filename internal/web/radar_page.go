package web

import (
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type categoryLink struct {
	Slug, Label, URL string
	Active           bool
}

type categoryCard struct {
	Slug        string
	Name        string
	Description string
	Count       int
	Children    []TopicRef
}

func categoryCards(items []TopicMetric, l localizer) []categoryCard {
	result := []categoryCard{}
	for _, item := range items {
		if item.ParentSlug != "" {
			continue
		}
		description := item.Description
		key := "category_description." + item.Slug
		if translated := l.Text(key); translated != key {
			description = translated
		}
		card := categoryCard{Slug: item.Slug, Name: l.TopicName(item.Slug, item.Name), Description: description, Count: item.RepositoryCount}
		for _, child := range items {
			if child.ParentSlug == item.Slug {
				card.Children = append(card.Children, TopicRef{Slug: child.Slug, Name: l.TopicName(child.Slug, child.Name)})
			}
		}
		result = append(result, card)
	}
	order := map[string]int{"coding-agents": 1, "research-agents": 2, "browser-computer-agents": 3, "workflow-agents": 4, "agent-platforms": 5, "ai-infrastructure": 6, "ai-agent": 7}
	sort.SliceStable(result, func(i, j int) bool {
		left, right := order[result[i].Slug], order[result[j].Slug]
		if left == 0 {
			left = 100
		}
		if right == 0 {
			right = 100
		}
		return left < right
	})
	return result
}

type boardRow struct {
	RadarRepository
	Rank     int
	BarWidth string
}
type leaderboard struct {
	Title, Description, Tone, URL string
	Rows                          []boardRow
	IsSlowdown                    bool
}
type directionSegment struct{ Class, Dash, Offset string }

func categoryLinks(path string, values url.Values) []categoryLink {
	result := []categoryLink{}
	for _, slug := range []string{"", "coding-agents", "research-agents", "browser-computer-agents", "workflow-agents", "agent-platforms", "ai-infrastructure", "ai-agent", "__unclassified"} {
		result = append(result, categoryLink{Slug: slug, Label: "category." + slug, URL: repositoryOptionURL(path, values, "topic", slug), Active: values.Get("topic") == slug})
	}
	return result
}

func (h *Handler) isStale(date time.Time) bool {
	if date.IsZero() {
		return false
	}
	today := h.now().In(h.location)
	boundary := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, h.location).AddDate(0, 0, -1)
	return date.Before(boundary)
}

func (h *Handler) radarHome(w http.ResponseWriter, r *http.Request) {
	l := h.localizerFor(r)
	if r.URL.Query().Get("new") == "1" || r.URL.Query().Get("q") != "" || r.URL.Query().Get("cursor") != "" {
		http.Redirect(w, r, queryPath("/repositories", r.URL.Query()), http.StatusFound)
		return
	}
	rawDate := strings.TrimSpace(r.URL.Query().Get("date"))
	filter := RepositoryQuery{AsOf: parseDateParameter(rawDate, h.location), WindowDays: normalizeRepositoryPeriod(r.URL.Query().Get("period")), TopicSlug: r.URL.Query().Get("topic"), OnlyFocus: r.URL.Query().Get("focus") == "1"}
	if rawDate != "" && filter.AsOf.IsZero() {
		h.badRequest(w, r, l.Text("error.invalid_date.title"), l.Text("error.invalid_date.message"))
		return
	}
	queryer, ok := h.queryer.(RadarQueryer)
	if !ok {
		h.repositoryIndex(w, r, "/repositories")
		return
	}
	data, err := queryer.RadarOverview(r.Context(), filter)
	if err != nil {
		h.serverError(w, r, err)
		return
	}
	values := cloneValues(r.URL.Query())
	values.Set("date", data.AsOf.Format("2006-01-02"))
	values.Set("period", fmt.Sprintf("%dd", data.Filter.WindowDays))
	view := pageView{
		Meta:         h.meta(l, "ui.dashboard_title", "ui.dashboard_description", "dashboard", nil),
		Radar:        data,
		TrendPeriods: repositoryPeriodOptions("/", values, data.Filter.WindowDays, l),
		Categories:   categoryLinks("/", values),
		CurrentPath:  "/",
	}
	view.Meta.AsOfLabel = formatDateLocalized(data.AsOf, h.location, l.Text("page.not_available"))
	newProjectValues := cloneValues(values)
	newProjectValues.Set("new", "1")
	newProjectValues.Set("period", "1d")
	newProjectValues.Set("sort", "stars")
	view.NewProjectsURL = queryPath("/repositories", newProjectValues)
	view.Meta.Stale = rawDate == "" && h.isStale(data.AsOf)
	makeBoard := func(title, description, tone, sort string, items []RadarRepository, slowdown bool, limit int) leaderboard {
		board := leaderboard{Title: l.Text(title), Description: l.Text(description), Tone: tone, URL: repositoryOptionURL("/repositories", values, "sort", sort), IsSlowdown: slowdown}
		if len(items) > limit {
			items = items[:limit]
		}
		maxValue := 0.0
		for _, item := range items {
			value := item.StarDelta
			if slowdown {
				value = item.MomentumChange
			}
			if value != nil {
				maxValue = math.Max(maxValue, math.Abs(float64(*value)))
			}
		}
		for i, item := range items {
			value := item.StarDelta
			if slowdown {
				value = item.MomentumChange
			}
			width := 0.0
			if value != nil && maxValue > 0 {
				width = math.Abs(float64(*value)) / maxValue * 100
			}
			board.Rows = append(board.Rows, boardRow{RadarRepository: item, Rank: i + 1, BarWidth: fmt.Sprintf("%.2f", width)})
		}
		return board
	}
	view.Boards = []leaderboard{
		makeBoard("dashboard.top_growth", "dashboard.top_growth_help", "positive", "delta", data.Fastest, false, 10),
		makeBoard("ui.slowdown", "dashboard.slowdown_help", "negative", "slowdown", data.FallingBehind, true, 6),
	}
	h.render(w, r, http.StatusOK, "home", view)
}

func radarHistoryChart(history []RadarHistoryPoint, l localizer) (chart, *float64, int) {
	input := make([]chartInput, 0, len(history))
	var change *float64
	cohort := 0
	for _, point := range history {
		var value *int64
		if point.Index != nil {
			v := int64(math.Round(*point.Index * 100))
			value = &v
			c := *point.Index - 100
			change = &c
		}
		cohort = point.CohortCount
		input = append(input, chartInput{Date: point.Date, Value: value})
	}
	c := makeChart(input, false, l.Text("ui.chart_title"), l.Text("ui.chart_help"))
	c.ID = "radar-history"
	if c.HasData {
		min, max := math.Inf(1), math.Inf(-1)
		for _, point := range history {
			if point.Index != nil {
				min = math.Min(min, *point.Index)
				max = math.Max(max, *point.Index)
			}
		}
		if max-min < 0.2 {
			padding := (0.2 - (max - min)) / 2
			min -= padding
			max += padding
		}
		c.MinLabel = fmt.Sprintf("%.2f", min)
		c.MaxLabel = fmt.Sprintf("%.2f", max)
		c.Segments = nil
		current := []string{}
		flush := func() {
			if len(current) > 0 {
				c.Segments = append(c.Segments, chartSegment{Points: strings.Join(current, " ")})
				current = nil
			}
		}
		pi := 0
		for i, point := range history {
			if point.Index != nil {
				if i > 0 && point.Date.Sub(history[i-1].Date) > 36*time.Hour {
					flush()
				}
				value := fmt.Sprintf("%.4f", *point.Index)
				c.Observations[i].Value = value
				c.Points[pi].Value = value
				c.Points[pi].Y = fmt.Sprintf("%.2f", 14+(1-(*point.Index-min)/(max-min))*184)
				current = append(current, c.Points[pi].X+","+c.Points[pi].Y)
				pi++
			} else {
				flush()
			}
		}
		flush()
	}
	return c, change, cohort
}

package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	htmlnode "golang.org/x/net/html"
)

// Read the rendered form's successful controls so these regressions exercise
// what a browser submits, rather than reconstructing the intended query by hand.
func libraryFilterForm(t *testing.T, body string) (*htmlnode.Node, url.Values) {
	t.Helper()
	doc, err := htmlnode.Parse(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	form := libraryFilterNode(doc, func(node *htmlnode.Node) bool {
		return node.Data == "form" && libraryFilterAttribute(node, "class") == "library-filters"
	})
	if form == nil {
		t.Fatal("missing project-library filter form")
	}
	values := url.Values{}
	var walk func(*htmlnode.Node)
	walk = func(node *htmlnode.Node) {
		name := libraryFilterAttribute(node, "name")
		if node.Type == htmlnode.ElementNode && name != "" {
			switch node.Data {
			case "input":
				values.Add(name, libraryFilterAttribute(node, "value"))
			case "select":
				option := libraryFilterNode(node, func(child *htmlnode.Node) bool {
					return child.Data == "option" && libraryFilterHasAttribute(child, "selected")
				})
				if option == nil {
					option = libraryFilterNode(node, func(child *htmlnode.Node) bool { return child.Data == "option" })
				}
				if option != nil {
					values.Add(name, libraryFilterAttribute(option, "value"))
				}
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(form)
	return form, values
}

func libraryFilterNode(node *htmlnode.Node, match func(*htmlnode.Node) bool) *htmlnode.Node {
	if node.Type == htmlnode.ElementNode && match(node) {
		return node
	}
	for child := node.FirstChild; child != nil; child = child.NextSibling {
		if found := libraryFilterNode(child, match); found != nil {
			return found
		}
	}
	return nil
}

func libraryFilterAttribute(node *htmlnode.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func libraryFilterHasAttribute(node *htmlnode.Node, name string) bool {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return true
		}
	}
	return false
}

func TestLibraryCombinedSearchAndTagSurviveRenderedFormSubmission(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			queryer.repositories.Items[0].Tags = []string{"ai"}
			handler := newTestHandlerWithLocale(t, queryer, locale)
			searched := request(t, handler, "/repositories?view=all&date=2026-08-30&q=audio.cpp&lang="+locale)
			tagLinks := parsedTagLinks(t, cardTagsMarkup(t, searched.Body.String()))
			var taggedURL string
			for _, link := range tagLinks {
				if link.URL.Query().Get("tag") == "ai" {
					taggedURL = link.URL.String()
				}
			}
			if taggedURL == "" {
				t.Fatal("missing clickable ai tag")
			}
			tagged := request(t, handler, taggedURL)
			for _, label := range []string{newLocalizer(locale).Textf("tags.active", "ai"), newLocalizer(locale).Textf("tags.search_active", "audio.cpp")} {
				if !strings.Contains(tagged.Body.String(), label) {
					t.Errorf("combined filter is not visibly identified: missing %q", label)
				}
			}
			form, values := libraryFilterForm(t, tagged.Body.String())
			search := libraryFilterNode(form, func(node *htmlnode.Node) bool {
				return libraryFilterAttribute(node, "id") == "repository-search"
			})
			if search == nil || libraryFilterAttribute(search, "name") != "q" || libraryFilterAttribute(search, "value") != "audio.cpp" {
				t.Error("visible search control must retain the search text alongside the active tag")
			}
			for _, key := range []string{"q", "tag"} {
				if len(values[key]) != 1 {
					t.Errorf("form must submit exactly one %s value, got %v", key, values[key])
				}
			}
			values.Set("period", "7d")
			response := request(t, handler, "/repositories?"+values.Encode())
			got := queryer.lastRepositoryQuery
			if response.Code != http.StatusOK || got.Search != "audio.cpp" || got.Tag != "ai" || got.WindowDays != 7 || got.OnlyNew || got.OnlyFocus {
				t.Fatalf("changing growth comparison lost the combined filter: %+v, status %d", got, response.Code)
			}
		})
	}
}

func TestLibraryClearFiltersRestoresDefaultsWithinTheCurrentView(t *testing.T) {
	type clearCase struct{ name, query, view, date, size string }
	tests := []clearCase{}
	for _, view := range []string{"daily", "all", "focus"} {
		tests = append(tests, clearCase{
			name: view, query: "view=" + view + "&date=2026-08-30&q=audio.cpp&tag=ai&topic=ai-agent&source=github_trending&status=paused&period=7d&sort=delta&page=2&size=6&lang=en", view: view, date: "2026-08-30", size: "6",
		})
	}
	tests = append(tests,
		clearCase{name: "legacy period and order", query: "date=2026-09-12&period=7d&sort=delta", view: "all", date: "2026-09-12"},
		clearCase{name: "legacy daily tag", query: "date=2026-09-12&new=1&tag=python", view: "daily", date: "2026-09-12"},
	)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			queryer := populatedFake()
			queryer.repositories.Total = 25
			queryer.repositories.Coverage.AsOfDate = mustDate(test.date)
			handler := newTestHandlerWithLocale(t, queryer, localeEnglish)
			response := request(t, handler, "/repositories?"+test.query)
			doc, err := htmlnode.Parse(strings.NewReader(response.Body.String()))
			if err != nil {
				t.Fatal(err)
			}
			clear := libraryFilterNode(doc, func(node *htmlnode.Node) bool {
				return node.Data == "a" && libraryFilterAttribute(node, "class") == "clear-filters"
			})
			if clear == nil || libraryFilterHasAttribute(clear, "hidden") {
				t.Fatal("active filters have no visible clear action")
			}
			location, err := url.Parse(libraryFilterAttribute(clear, "href"))
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"q", "tag", "topic", "source", "status", "page", "cursor"} {
				if location.Query().Has(key) {
					t.Errorf("clear retained %s: %s", key, location)
				}
			}
			for key, want := range map[string]string{"view": test.view, "date": test.date, "lang": "en", "period": "1d", "sort": "stars", "size": test.size} {
				if location.Query().Get(key) != want {
					t.Errorf("clear has %s=%q, want %q: %s", key, location.Query().Get(key), want, location)
				}
			}
			cleared := request(t, handler, location.String())
			got := queryer.lastRepositoryQuery
			if got.Search != "" || got.Tag != "" || got.TopicSlug != "" || got.Source != "" || got.MonitoringStatus != "" || got.WindowDays != 1 || got.Sort != "stars" || got.Offset != 0 || got.AfterID != nil || got.OnlyNew != (test.view == "daily") || got.OnlyFocus != (test.view == "focus") {
				t.Errorf("clear did not restore the current view's default query: %+v", got)
			}
			clearedDoc, _ := htmlnode.Parse(strings.NewReader(cleared.Body.String()))
			clearedAction := libraryFilterNode(clearedDoc, func(node *htmlnode.Node) bool {
				return node.Data == "a" && libraryFilterAttribute(node, "class") == "clear-filters"
			})
			if clearedAction == nil || !libraryFilterHasAttribute(clearedAction, "hidden") {
				t.Error("clear action remained visible after every clearable filter was removed")
			}
		})
	}
}

func TestLibrarySortOptionsRemainAvailableAfterChangingOrder(t *testing.T) {
	queryer := populatedFake()
	handler := newTestHandler(t, queryer)
	orders := []string{"velocity", "delta", "growth_rate", "low_growth", "slowdown", "rank_change", "stars", "name", "newest"}
	for _, order := range orders {
		t.Run(order, func(t *testing.T) {
			response := request(t, handler, "/repositories?view=all&sort="+order)
			form, values := libraryFilterForm(t, response.Body.String())
			selectNode := libraryFilterNode(form, func(node *htmlnode.Node) bool {
				return node.Data == "select" && libraryFilterAttribute(node, "name") == "sort"
			})
			if selectNode == nil || values.Get("sort") != order {
				t.Fatalf("selected order %q was not rendered", order)
			}
			for _, available := range orders {
				option := libraryFilterNode(selectNode, func(node *htmlnode.Node) bool {
					return node.Data == "option" && libraryFilterAttribute(node, "value") == available
				})
				if option == nil {
					t.Errorf("order %s disappeared after selecting %s", available, order)
				}
			}
		})
	}
}

package web

import (
	"html"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"testing"
)

func TestRepositoryIndexUsesOffsetAndRedirectsToLastValidPage(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Total = 13
	handler := newTestHandler(t, queryer)
	response := request(t, handler, "/repositories?view=all&new=0&page=4&size=6&q=agent&tag=skills&date=2026-08-30&period=30d&sort=name&lang=zh-CN")
	if response.Code != http.StatusFound {
		t.Fatalf("out-of-range page status = %d, want %d", response.Code, http.StatusFound)
	}
	if queryer.lastRepositoryQuery.Limit != 6 || queryer.lastRepositoryQuery.Offset != 18 || queryer.lastRepositoryQuery.AfterID != nil {
		t.Fatalf("numbered page did not reach query layer: %+v", queryer.lastRepositoryQuery)
	}
	location, err := url.Parse(html.UnescapeString(response.Header().Get("Location")))
	if err != nil {
		t.Fatal(err)
	}
	if location.Path != "/repositories" || location.Query().Get("page") != "3" || location.Query().Get("size") != "6" || location.Query().Has("cursor") {
		t.Fatalf("last-page redirect = %s", location)
	}
	for key, want := range map[string]string{
		"view": "all", "focus": "0", "new": "0", "q": "agent", "tag": "skills",
		"date": "2026-08-30", "period": "30d", "sort": "name", "lang": localeChinese,
	} {
		if location.Query().Get(key) != want {
			t.Fatalf("redirect lost %s: %s", key, location)
		}
	}

	response = request(t, handler, location.String())
	if response.Code != http.StatusOK || queryer.lastRepositoryQuery.Offset != 12 || queryer.lastRepositoryQuery.Limit != 6 {
		t.Fatalf("final page query = %+v, status %d", queryer.lastRepositoryQuery, response.Code)
	}
}

func TestRepositoryPaginationBuildsCompactStatefulLinks(t *testing.T) {
	values := url.Values{
		"view":   {"focus"},
		"focus":  {"1"},
		"new":    {"0"},
		"date":   {"2026-08-30"},
		"period": {"30d"},
		"sort":   {"name"},
		"tag":    {"skills"},
		"q":      {"agent"},
		"lang":   {localeChinese},
		"size":   {"6"},
		"page":   {"99"},
		"cursor": {"legacy"},
	}
	page := repositoryPagination("/repositories", values, 5, 6, 120)
	if page.Start != 25 || page.End != 30 || page.Total != 120 || page.CurrentPage != 5 || page.PageCount != 20 || page.PageSize != 6 {
		t.Fatalf("pagination metadata = %+v", page)
	}
	assertPageURL := func(raw string, wantPage int) {
		t.Helper()
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if parsed.Query().Has("cursor") || parsed.Query().Get("page") != strconv.Itoa(wantPage) {
			t.Fatalf("page URL did not replace old navigation state: %s", parsed)
		}
		for key, want := range map[string]string{
			"view": "focus", "focus": "1", "new": "0", "date": "2026-08-30",
			"period": "30d", "sort": "name", "tag": "skills", "q": "agent",
			"lang": localeChinese, "size": "6",
		} {
			if parsed.Query().Get(key) != want {
				t.Fatalf("page URL lost %s: %s", key, parsed)
			}
		}
	}
	assertPageURL(page.PreviousURL, 4)
	assertPageURL(page.NextURL, 6)

	compact := make([]int, 0, len(page.Pages))
	ellipses := 0
	for _, item := range page.Pages {
		if item.Ellipsis {
			ellipses++
			continue
		}
		compact = append(compact, item.Number)
		if item.Current != (item.Number == 5) {
			t.Fatalf("wrong current page marker: %+v", item)
		}
	}
	if !reflect.DeepEqual(compact, []int{1, 4, 5, 6, 20}) || ellipses != 2 {
		t.Fatalf("compact pages = %v with %d ellipses", compact, ellipses)
	}
	if len(page.Sizes) != 3 || !page.Sizes[0].Selected || page.Sizes[1].Selected || page.Sizes[2].Selected {
		t.Fatalf("size options = %+v", page.Sizes)
	}
	for _, option := range page.Sizes {
		parsed, err := url.Parse(option.URL)
		if err != nil || parsed.Query().Has("page") || parsed.Query().Has("cursor") {
			t.Fatalf("size URL retained pagination state: %s", option.URL)
		}
	}
}

func TestRepositoryPaginationDefaultsAndEmptyRange(t *testing.T) {
	for raw, want := range map[string]int{"": 20, "5": 20, "6": 6, "12": 12, "20": 20, " 6 ": 6} {
		if got := normalizeRepositoryPageSize(raw); got != want {
			t.Fatalf("size %q = %d, want %d", raw, got, want)
		}
	}
	page := repositoryPagination("/repositories", nil, 10, 20, 0)
	if page.Start != 0 || page.End != 0 || page.CurrentPage != 1 || page.PageCount != 1 || len(page.Pages) != 1 || !page.Pages[0].Current {
		t.Fatalf("empty pagination = %+v", page)
	}
}

package web

import (
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

type parsedTagLink struct {
	URL   *url.URL
	Label string
}

func TestCommonTagLinksOnlyOfferAvailableValuesAndKeepScope(t *testing.T) {
	values := url.Values{"date": {"2026-09-08"}, "new": {"1"}, "period": {"7d"}, "q": {"agent"}, "cursor": {"2s"}, "tag": {" SKILLS "}}
	links := suggestedTagLinks([]TagRef{{Name: "investment"}, {Name: "skills"}}, values, newLocalizer(localeChinese))
	if len(links) != 2 || links[0].Label != "投资 · investment" || !links[1].Active {
		t.Fatalf("common tags should be grounded in available values: %+v", links)
	}
	for _, link := range links {
		location, err := url.Parse(link.URL)
		if err != nil || location.Query().Has("cursor") || location.Query().Get("date") != "2026-09-08" || location.Query().Get("new") != "1" || location.Query().Get("q") != "agent" {
			t.Fatalf("common tag lost its selected scope: %s, %v", link.URL, err)
		}
	}
}

func TestSelectedTagStaysVisibleOutsideTheInitialEight(t *testing.T) {
	tags := []string{"a", "b", "c", "d", "e", "f", "g", "h", "investment"}
	links := makeCardTagLinks(tags, nil, url.Values{"tag": {"investment"}}, newLocalizer(localeChinese))
	if len(links.Visible) != 8 || len(links.More) != 1 || !links.Visible[0].Active || links.Visible[0].Label != "investment" {
		t.Fatalf("selected tag hidden in overflow: %+v", links)
	}
	links = makeCardTagLinks(nil, []TopicRef{{Slug: "skills", Name: "技能"}}, url.Values{"tag": {"技能"}}, newLocalizer(localeChinese))
	if len(links.Visible) != 1 || !links.Visible[0].Active {
		t.Fatalf("selected taxonomy name not highlighted: %+v", links)
	}
	links = makeCardTagLinks([]string{"skills"}, []TopicRef{{Slug: "skills", Name: "技能"}}, url.Values{"tag": {"技能"}}, newLocalizer(localeChinese))
	if len(links.Visible) != 1 || !links.Visible[0].Active {
		t.Fatalf("taxonomy alias lost when deduplicated with raw tag: %+v", links)
	}
}

func TestEmptyDailyTagViewCanBroadenWithoutDroppingTheTag(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Items, queryer.repositories.Total = nil, 0
	response := request(t, newTestHandlerWithLocale(t, queryer, localeChinese), "/repositories?view=daily&new=1&tag=investment&date=2026-08-30&lang=zh-CN")
	found := false
	for _, link := range parsedTagLinks(t, response.Body.String()) {
		if link.Label == newLocalizer(localeChinese).Text("tags.browse_all_matching") {
			found = true
			if link.URL.Query().Get("view") != "all" || link.URL.Query().Get("new") != "0" || link.URL.Query().Get("tag") != "investment" || link.URL.Query().Get("date") != "2026-08-30" {
				t.Fatalf("empty daily exit lost the tag: %s", link.URL)
			}
		}
	}
	if !found {
		t.Fatal("missing scoped full-library exit")
	}
}

func TestRootTagLinksRedirectToTheLibraryWithTheirFullQuery(t *testing.T) {
	queryer := populatedFake()
	values := url.Values{"tag": {"投资"}, "date": {"2026-08-30"}, "period": {"30d"}, "focus": {"1"}, "new": {"0"}, "q": {""}, "sort": {"stars"}, "lang": {"zh-CN"}}
	response := request(t, newTestHandler(t, queryer), "/?"+values.Encode())
	location, err := url.Parse(response.Header().Get("Location"))
	if response.Code != http.StatusFound || err != nil || location.Path != "/repositories" || location.Query().Encode() != values.Encode() {
		t.Fatalf("root tag URL lost its library context: status=%d location=%s error=%v", response.Code, response.Header().Get("Location"), err)
	}
	if queryer.lastRepositoryQuery.Limit != 0 || !queryer.lastRadarQuery.AsOf.IsZero() {
		t.Fatal("compatibility redirect should not execute a data query")
	}
}

func parsedTagLinks(t *testing.T, markup string) []parsedTagLink {
	t.Helper()
	matches := regexp.MustCompile(`(?s)<a\b[^>]*href="([^"]+)"[^>]*>(.*?)</a>`).FindAllStringSubmatch(markup, -1)
	links := make([]parsedTagLink, 0, len(matches))
	for _, match := range matches {
		location, err := url.Parse(html.UnescapeString(match[1]))
		if err != nil {
			t.Fatal(err)
		}
		links = append(links, parsedTagLink{URL: location, Label: html.UnescapeString(match[2])})
	}
	return links
}

func cardTagsMarkup(t *testing.T, body string) string {
	t.Helper()
	card := feedCards(t, body)[0]
	match := regexp.MustCompile(`(?s)<div class="project-card-topics">(.*?)</div>\s*<footer`).FindStringSubmatch(card)
	if len(match) != 2 {
		t.Fatal("project card has no tag block")
	}
	return match[1]
}

func TestTagFilterAndGrowthLabelsReplaceTheOldSelectorsInBothLanguages(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		queryer := populatedFake()
		queryer.repositories.Tags = []TagRef{{Name: "投资", Count: 12}, {Name: "agent", Count: 7}, {Name: "skills", Count: 3}, {Name: "saas", Count: 2}}
		handler := newTestHandlerWithLocale(t, queryer, locale)
		response := request(t, handler, "/repositories?view=all&lang="+locale)
		if response.Code != http.StatusOK {
			t.Fatalf("tag page returned %d", response.Code)
		}
		body := html.UnescapeString(response.Body.String())
		l := newLocalizer(locale)
		for _, expected := range []string{`<label for="tag-filter">` + l.Text("tags.filter") + `</label>`, `list="repository-tags"`, `<datalist id="repository-tags">`, `maxlength="80"`, `id="sort-filter"`, `id="as-of-date"`, l.Text("tags.period"), l.Text("tags.1d"), l.Text("tags.7d"), l.Text("tags.30d"), l.Text("tags.period_help"), l.Text("tags.method_note")} {
			if !strings.Contains(body, expected) {
				t.Errorf("%s tag/growth UI missing %q", locale, expected)
			}
		}
		for _, value := range []string{"投资", "agent", "skills", "saas"} {
			if !strings.Contains(body, `value="`+value+`"`) {
				t.Errorf("globally available label %q missing", value)
			}
		}
		if regexp.MustCompile(`<select\b[^>]*name="(?:source|topic)"`).MatchString(body) || strings.Contains(body, `class="category-tabs"`) {
			t.Fatal("obsolete category/source controls remain in the library")
		}
		if regexp.MustCompile(`<input\b[^>]*name="source"`).MatchString(body) || strings.Contains(body, l.Textf("tags.legacy_source", "GitHub Trending")) {
			t.Fatal("a source filter was introduced on a normal new URL")
		}
	}
}

func TestTagQueryAndClickableCardLabelsKeepTheSelectedContext(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Items[0].Tags = []string{"skills", "投资"}
	queryer.repositories.Items[0].Topics = []TopicRef{{Slug: "skills", Name: "Skills"}}
	values := url.Values{"tag": {"  SKILLS  "}, "date": {"2026-08-30"}, "period": {"30d"}, "focus": {"1"}, "new": {"1"}, "q": {"agent"}, "sort": {"stars"}, "source": {"github_trending"}, "cursor": {"2s"}, "lang": {"zh-CN"}}
	response := request(t, newTestHandler(t, queryer), "/repositories?"+values.Encode())
	got := queryer.lastRepositoryQuery
	if response.Code != http.StatusOK || got.Tag != "SKILLS" || got.AsOf.Format("2006-01-02") != "2026-08-30" || got.WindowDays != 30 || !got.OnlyFocus || !got.OnlyNew || got.Search != "agent" || got.Sort != "stars" {
		t.Fatalf("tag was not passed to the scoped query: %+v, %d", got, response.Code)
	}
	markup := cardTagsMarkup(t, response.Body.String())
	links := parsedTagLinks(t, markup)
	if len(links) != 2 {
		t.Fatalf("raw/taxonomy tags were not deduplicated: %d", len(links))
	}
	if !strings.Contains(markup, `class="tag tag-selected"`) {
		t.Fatal("normalized selected label is not highlighted")
	}
	for _, link := range links {
		if link.URL.Path != "/repositories" || link.URL.Query().Has("cursor") {
			t.Fatalf("tag link did not reset pagination: %s", link.URL)
		}
		for key, expected := range map[string]string{"date": "2026-08-30", "period": "30d", "focus": "1", "new": "1", "q": "agent", "sort": "stars", "source": "github_trending", "lang": "zh-CN"} {
			if link.URL.Query().Get(key) != expected {
				t.Errorf("tag link lost %s: %s", key, link.URL)
			}
		}
		if selected := link.URL.Query().Get("tag"); selected != "skills" && selected != "投资" {
			t.Fatalf("tag link selected unexpected label: %s", link.URL)
		}
	}
}

func TestTagAndLegacyFilterBadgesAreIndependentlyClearable(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		queryer := populatedFake()
		handler := newTestHandlerWithLocale(t, queryer, locale)
		values := url.Values{"tag": {"skills"}, "topic": {"ai-agent"}, "source": {"github_trending"}, "date": {"2026-08-30"}, "period": {"7d"}, "focus": {"1"}, "new": {"1"}, "q": {"agent"}, "sort": {"growth_rate"}, "cursor": {"2s"}, "lang": {locale}}
		response := request(t, handler, "/repositories?"+values.Encode())
		body := html.UnescapeString(response.Body.String())
		badge := regexp.MustCompile(`(?s)<div class="library-active-filters">(.*?)</div>`).FindString(body)
		links := parsedTagLinks(t, badge)
		if len(links) != 3 {
			t.Fatalf("expected tag/category/legacy-source badges, got %d", len(links))
		}
		l := newLocalizer(locale)
		for index, key := range []string{"tag", "topic", "source"} {
			link := links[index]
			if link.URL.Query().Has(key) || link.URL.Query().Has("cursor") {
				t.Fatalf("clear %s retained that filter or the old cursor: %s", key, link.URL)
			}
			for kept, expected := range values {
				if kept == key || kept == "cursor" {
					continue
				}
				if link.URL.Query().Get(kept) != expected[0] {
					t.Errorf("clear %s lost %s: %s", key, kept, link.URL)
				}
			}
			if !strings.Contains(link.Label, l.Text("tags.clear")) {
				t.Fatal("clear action has no accessible label")
			}
		}
		if !strings.Contains(badge, l.Textf("tags.legacy_source", "GitHub Trending")) {
			t.Fatal("legacy source restriction is hidden from the reader")
		}
		for _, key := range []string{"topic", "source"} {
			if !regexp.MustCompile(`<input\b[^>]*type="hidden"[^>]*name="` + key + `"`).MatchString(body) {
				t.Fatalf("submitting a different filter would silently drop legacy %s", key)
			}
		}
		if regexp.MustCompile(`<select\b[^>]*name="source"`).MatchString(body) {
			t.Fatal("legacy compatibility resurrected the source selector")
		}
		request(t, handler, links[0].URL.String())
		if queryer.lastRepositoryQuery.Tag != "" || !queryer.lastRepositoryQuery.OnlyFocus || !queryer.lastRepositoryQuery.OnlyNew {
			t.Fatal("clearing tag changed another filter")
		}
	}
}

func TestCardExtraTagsStayCompleteAndClickableIncludingSaaS(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.Items[0].Tags = []string{"agent", "skills", "投资", "rag", "mcp", "memory", "cli", "observability", "SaaS", "finance", "data"}
	queryer.repositories.Items[0].Topics = []TopicRef{{Slug: "skills", Name: "Skills"}, {Slug: "coding-agents", Name: "Coding Agents"}}
	response := request(t, newTestHandler(t, queryer), "/repositories?view=all&date=2026-08-30&lang=en")
	markup := cardTagsMarkup(t, response.Body.String())
	parts := strings.SplitN(markup, `<details class="project-more-tags">`, 2)
	if len(parts) != 2 {
		t.Fatal("extra labels have no native details expansion")
	}
	if len(parsedTagLinks(t, parts[0])) != 8 || len(parsedTagLinks(t, parts[1])) != 4 {
		t.Fatal("the first eight/remaining four label split lost labels")
	}
	if !strings.Contains(parts[1], "Show 4 more tags") || !strings.Contains(parts[1], ">SaaS</a>") {
		t.Fatal("SaaS was dropped beyond the initial visible labels")
	}
	keys := map[string]bool{}
	for _, link := range parsedTagLinks(t, markup) {
		key := link.URL.Query().Get("tag")
		if key == "" || keys[key] || link.URL.Path != "/repositories" {
			t.Fatalf("missing, duplicated, or invalid label link: %s", link.URL)
		}
		keys[key] = true
	}
	for _, key := range []string{"agent", "skills", "投资", "rag", "mcp", "memory", "cli", "observability", "saas", "finance", "data", "coding-agents"} {
		if !keys[key] {
			t.Errorf("complete card tag set lost %q", key)
		}
	}
}

func TestTagValidationBoundsInputAndRejectsControlCharactersWithoutQuerying(t *testing.T) {
	for _, value := range []string{strings.Repeat("投", 81), "a\x00b", "skill\nother", "in\tvestment", "x\x7fy", "invalid\xff"} {
		queryer := populatedFake()
		response := request(t, newTestHandlerWithLocale(t, queryer, localeChinese), "/repositories?tag="+url.QueryEscape(value))
		if response.Code != http.StatusBadRequest || queryer.lastRepositoryQuery.Limit != 0 {
			t.Fatalf("invalid tag reached the query: %q, status=%d", value, response.Code)
		}
		if !strings.Contains(response.Body.String(), "标签格式不正确") || !strings.Contains(response.Body.String(), "80") {
			t.Fatal("invalid tag has no actionable Chinese explanation")
		}
	}
	queryer := populatedFake()
	valid := strings.Repeat("投", 80)
	response := request(t, newTestHandler(t, queryer), "/repositories?tag="+url.QueryEscape(valid))
	if response.Code != http.StatusOK || queryer.lastRepositoryQuery.Tag != valid || queryer.lastRepositoryQuery.OnlyNew {
		t.Fatal("80 Unicode characters were truncated, rejected, or narrowed to today's additions")
	}
}

func TestTagsEscapeHostileTextAndStayOnInternalFilterLinks(t *testing.T) {
	queryer := populatedFake()
	hostile := `<img src=x onerror="alert('tag')">`
	queryer.repositories.Items[0].Tags = []string{hostile}
	queryer.repositories.Items[0].Topics = nil
	queryer.repositories.Tags = []TagRef{{Name: hostile, Count: 1}}
	body := request(t, newTestHandler(t, queryer), "/repositories?tag="+url.QueryEscape(hostile)).Body.String()
	if strings.Contains(body, "<img src=x") || strings.Contains(body, `<script>alert`) || !strings.Contains(body, "&lt;img") {
		t.Fatal("tag or datalist text was not safely escaped")
	}
	links := parsedTagLinks(t, cardTagsMarkup(t, body))
	if len(links) != 1 || links[0].URL.IsAbs() || links[0].URL.Path != "/repositories" || links[0].URL.Query().Get("tag") != strings.ToLower(hostile) {
		t.Fatalf("hostile tag escaped the internal query URL: %+v", links)
	}
}

func TestRepositoryDetailShowsAllClickableTagFilters(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		queryer := populatedFake()
		queryer.repository.Repository.Tags = []string{"agent", "skills", "投资", "rag", "mcp", "memory", "cli", "observability", "saas", "finance"}
		queryer.repository.Repository.Topics = []TopicRef{{Slug: "skills", Name: "Skills"}}
		response := request(t, newTestHandlerWithLocale(t, queryer, locale), "/repositories/101?date=2026-08-30&lang="+locale)
		if response.Code != http.StatusOK {
			t.Fatalf("detail returned %d", response.Code)
		}
		links := parsedTagLinks(t, response.Body.String())
		found := map[string]bool{}
		for _, link := range links {
			if !link.URL.Query().Has("tag") {
				continue
			}
			if link.URL.Path != "/repositories" || link.URL.Query().Get("view") != "all" || link.URL.Query().Get("date") != "2026-08-30" || link.URL.Query().Get("lang") != locale {
				t.Fatalf("detail tag lost the selected context: %s", link.URL)
			}
			found[link.URL.Query().Get("tag")] = true
		}
		if len(found) != 10 || !found["saas"] || !found["投资"] {
			t.Fatalf("detail discarded extra or Chinese tags: %+v", found)
		}
	}
}

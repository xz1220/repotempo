package web

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestLibraryDetailReturnContextRoundTrip(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			queryer := populatedFake()
			handler := newTestHandler(t, queryer)
			values := url.Values{
				"cursor": {"az"}, "date": {"2026-08-30"}, "period": {"30d"}, "sort": {"stars"},
				"tag": {"中文写作"}, "topic": {"coding-agent"}, "source": {"github_trending"},
				"status": {"active"}, "q": {"C++ a+b&c% 工具"}, "new": {"0"}, "focus": {"1"},
				"view": {"focus"}, "lang": {locale},
			}
			body := request(t, handler, queryPath("/repositories", values)).Body.String()
			listURL := navigationAttribute(t, body, `data-list-url="([^"]+)"`)
			if listURL != queryPath("/repositories", values) {
				t.Fatalf("canonical list URL lost effective scope: %s", listURL)
			}
			links := regexp.MustCompile(`href="([^"]+)" data-repository-detail`).FindAllStringSubmatch(body, -1)
			if len(links) != 2 || links[0][1] != links[1][1] {
				t.Fatalf("title and details do not share their return context: %v", links)
			}
			detailURL, err := url.Parse(html.UnescapeString(links[0][1]))
			if err != nil {
				t.Fatal(err)
			}
			want := listURL + "#project-101"
			if detailURL.Query().Get("return_to") != want || detailURL.Query().Get("date") != "2026-08-30" || detailURL.Query().Get("lang") != locale {
				t.Fatalf("detail URL lost state: %s", detailURL)
			}
			detail := request(t, handler, detailURL.String())
			returns := regexp.MustCompile(`href="([^"]+)"\s+data-library-return`).FindAllStringSubmatch(detail.Body.String(), -1)
			if len(returns) != 2 {
				t.Fatalf("want contextual sidebar and breadcrumb: %v", returns)
			}
			for _, link := range returns {
				if html.UnescapeString(link[1]) != want {
					t.Fatalf("return URL = %s; want %s", link[1], want)
				}
			}
			request(t, handler, want)
			filter := queryer.lastRepositoryQuery
			if filter.AfterID == nil || *filter.AfterID != 395 || !filter.OnlyFocus || filter.OnlyNew || filter.Sort != "stars" || filter.WindowDays != 30 || filter.Tag != "中文写作" || filter.Search != values.Get("q") || filter.Source != "github_trending" {
				t.Fatalf("roundtrip changed the actual query: %+v", filter)
			}
			if strings.Contains(body, "data-library-return") {
				t.Fatal("ordinary library sidebar must retain its default entry")
			}
		})
	}
}

func TestLibraryReturnFreezesImplicitDefaultsAndDoesNotCopyUnknownQuery(t *testing.T) {
	h := &Handler{location: time.UTC, now: func() time.Time { return mustDate("2026-09-08") }}
	values := url.Values{"lang": {localeChinese}, "tracking": {"ignored"}}
	filter := RepositoryQuery{WindowDays: 7, Sort: "velocity"}
	h.applyLibraryDefaults(values, &filter)
	canonical := h.canonicalLibraryURL(values, filter, localeChinese)
	parsed, _ := url.Parse(canonical)
	want := url.Values{"date": {"2026-09-08"}, "period": {"1d"}, "sort": {"stars"}, "new": {"1"}, "focus": {"0"}, "view": {"all"}, "lang": {localeChinese}}
	if !reflect.DeepEqual(parsed.Query(), want) {
		t.Fatalf("defaults not recorded: %s", canonical)
	}
	h.now = func() time.Time { return mustDate("2026-09-09") }
	replayed := RepositoryQuery{AsOf: parseDateParameter(parsed.Query().Get("date"), time.UTC), WindowDays: normalizeRepositoryPeriod(parsed.Query().Get("period")), Sort: normalizeRepositorySort(parsed.Query().Get("sort")), OnlyNew: parsed.Query().Get("new") == "1"}
	h.applyLibraryDefaults(parsed.Query(), &replayed)
	if replayed.AsOf.Format("2006-01-02") != "2026-09-08" || !replayed.OnlyNew || replayed.WindowDays != 1 || replayed.Sort != "stars" {
		t.Fatalf("return drifted across midnight: %+v", replayed)
	}
}

func TestLibraryReturnValidation(t *testing.T) {
	for _, good := range []string{
		"/repositories", "/repositories#project-101", "/repositories?cursor=az&new=0&lang=zh-CN#project-101",
		"/repositories?q=" + url.QueryEscape("C++ 中文 & percent%20 literal") + "&tag=" + url.QueryEscape("文档"),
	} {
		if _, ok := validatedLibraryReturnURL(good); !ok {
			t.Errorf("rejected safe destination %q", good)
		}
	}
	for _, bad := range []string{
		"", "https://example.com/repositories", "//example.com/repositories", "javascript:alert(1)",
		"repositories", "/repositories/", "/repositories/101", "/other", "/%72epositories", "/%2572epositories",
		"/repositories%2f..%2fother", "/repositories\\evil", "/repositories?q=%5cevil", "/repositories?q=%0aevil", "/repositories?q=\x00", "/repositories?q=%ff",
		"/repositories?return_to=%2Frepositories", "/repositories?%72eturn_to=x", "/repositories?%2572eturn_to=x", "/repositories?unknown=x",
		"/repositories?new=1&%6eew=0", "/repositories?cursor=-1", "/repositories?period=3d", "/repositories?lang=fr", "/repositories?date=2026-02-30",
		"/repositories?q=%", "/repositories?q=x;y=z", "/repositories#evil", "/repositories#project-0", "/repositories#project--1", "/repositories#project-001", "/repositories#project-9223372036854775808",
		"/repositories?q=" + strings.Repeat("x", maxLibraryReturnBytes),
		"/repositories?q=" + strings.Repeat("文", 1000),
	} {
		if got, ok := validatedLibraryReturnURL(bad); ok {
			t.Errorf("accepted unsafe destination %q as %q", bad, got)
		}
	}
}

func TestLibraryReturnLegacyReferrerAndExplicitInvalid(t *testing.T) {
	h := &Handler{}
	for _, test := range []struct{ name, target, referrer, want string }{
		{"same origin legacy", "/repositories/101", "http://example.com/repositories?cursor=az&date=2026-08-30&lang=en", "/repositories?cursor=az&date=2026-08-30&lang=en#project-101"},
		{"old daily freezes detail date", "/repositories/101?date=2026-08-30", "http://example.com/repositories?lang=zh-CN", "/repositories?date=2026-08-30&lang=zh-CN&new=1&period=1d&sort=stars&view=all#project-101"},
		{"old bare daily freezes detail date", "/repositories/101?date=2026-08-30", "http://example.com/repositories", "/repositories?date=2026-08-30&new=1&period=1d&sort=stars&view=all#project-101"},
		{"old explicit scope not narrowed", "/repositories/101?date=2026-08-30", "http://example.com/repositories?cursor=az", "/repositories?cursor=az#project-101"},
		{"external", "/repositories/101", "http://evil.test/repositories?cursor=az", "/repositories"},
		{"different port", "/repositories/101", "http://example.com:8080/repositories?cursor=az", "/repositories"},
		{"different scheme", "/repositories/101", "https://example.com/repositories?cursor=az", "/repositories"},
		{"nonlibrary", "/repositories/101", "http://example.com/runs", "/repositories"},
		{"absent", "/repositories/101", "", "/repositories"},
		{"explicit bad no bypass", "/repositories/101?return_to=https://evil.test", "http://example.com/repositories?cursor=az", "/repositories"},
		{"explicit empty no bypass", "/repositories/101?return_to=", "http://example.com/repositories?cursor=az", "/repositories"},
		{"malformed no bypass", "/repositories/101?return_to=%zz", "http://example.com/repositories?cursor=az", "/repositories"},
		{"duplicate no bypass", "/repositories/101?return_to=%2Frepositories&return_to=%2Frepositories", "http://example.com/repositories?cursor=az", "/repositories"},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://example.com"+test.target, nil)
			r.Header.Set("Referer", test.referrer)
			if got := h.libraryReturnURL(r, 101); got != test.want {
				t.Fatalf("return URL = %q; want %q", got, test.want)
			}
		})
	}
}

func TestDetailLanguageSwitchRetainsValidatedLibraryDestination(t *testing.T) {
	handler := newTestHandler(t, populatedFake())
	r := httptest.NewRequest(http.MethodGet, "http://example.com/repositories/101?lang=zh-CN", nil)
	r.Header.Set("Referer", "http://example.com/repositories?cursor=az&date=2026-08-30&new=0&lang=zh-CN")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	link := navigationAttribute(t, w.Body.String(), `href="([^"]+)" lang="en"`)
	parsed, err := url.Parse(link)
	if err != nil || parsed.Query().Get("lang") != localeEnglish {
		t.Fatalf("bad language URL: %s", link)
	}
	back, _ := url.Parse(parsed.Query().Get("return_to"))
	if back.Query().Get("cursor") != "az" || back.Query().Get("lang") != localeEnglish || back.Query().Get("date") != "2026-08-30" || back.Fragment != "project-101" {
		t.Fatalf("language switch lost return context: %s", back)
	}
	response := request(t, handler, link)
	if strings.Count(response.Body.String(), "data-library-return") != 2 || !strings.Contains(html.UnescapeString(response.Body.String()), `href="`+back.String()+`"`) {
		t.Fatal("switched detail no longer returns to its originating list")
	}
}

func navigationAttribute(t *testing.T, body, pattern string) string {
	t.Helper()
	match := regexp.MustCompile(pattern).FindStringSubmatch(body)
	if len(match) != 2 {
		t.Fatalf("missing navigation attribute %s", pattern)
	}
	return html.UnescapeString(match[1])
}

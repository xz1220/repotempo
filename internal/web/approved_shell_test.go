package web

import (
	"html"
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestApprovedShellKeepsFrozenChromeWithoutDemoCapabilities(t *testing.T) {
	handler := newTestHandlerWithLocale(t, populatedFake(), localeChinese)
	body := html.UnescapeString(request(t, handler, "/repositories?view=all&new=0&lang=zh-CN").Body.String())

	sidebar := regexp.MustCompile(`(?s)<aside class="app-sidebar".*?</aside>`).FindString(body)
	primary := regexp.MustCompile(`(?s)<nav class="primary-nav".*?</nav>`).FindString(sidebar)
	if strings.Count(primary, `class="nav-link`) != 2 || !strings.Contains(primary, ">趋势看板<") || !strings.Contains(primary, ">GitHub 项目<") {
		t.Fatalf("approved sidebar must contain exactly the two primary destinations: %s", primary)
	}
	if strings.Contains(sidebar, `class="sidebar-label"`) || strings.Contains(sidebar, "开源趋势观察") {
		t.Fatal("removed sidebar captions or promotional copy returned")
	}
	if !strings.Contains(sidebar, `href="https://github.com/xz1220/repotempo" target="_blank" rel="noopener noreferrer"`) || !strings.Contains(sidebar, ">Star on GitHub<") {
		t.Fatal("safe plain-text source repository link is missing")
	}
	if strings.Count(body, `href="/watch/new"`) != 1 || !strings.Contains(body, `class="button button-primary header-add"`) {
		t.Fatal("project library must expose one real Add project action in the header")
	}
	if !strings.Contains(body, `class="button button-secondary header-export" href="/repositories/export?`) || !strings.Contains(body, `class="account-menu-disabled" aria-disabled="true"`) {
		t.Fatal("Web export must use the server route; key access requires configured sign-in")
	}
	for _, forbidden := range []string{"type=\"password\"", "注册 RepoTempo", "ak_demo_", "sk_demo_", "/api/v1/"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("production shell contains demo-only capability %q", forbidden)
		}
	}
}

func TestGitHubProjectsNavigationOmitsCountButLibraryKeepsTotals(t *testing.T) {
	queryer := populatedFake()
	queryer.repositories.RegistryTotal = 1_250
	queryer.repositories.Total = 1_250
	handler := newTestHandlerWithLocale(t, queryer, localeChinese)
	response := request(t, handler, "/repositories?view=all&new=0&lang=zh-CN")
	if response.Code != http.StatusOK {
		t.Fatalf("project library returned %d", response.Code)
	}
	body := html.UnescapeString(response.Body.String())
	primary := regexp.MustCompile(`(?s)<nav class="primary-nav".*?</nav>`).FindString(body)
	link := regexp.MustCompile(`(?s)<a\b[^>]*href="/repositories[^"]*"[^>]*>(.*?)</a>`).FindStringSubmatch(primary)
	if len(link) != 2 || !strings.Contains(link[1], "<svg") || !strings.Contains(link[1], "<span>GitHub 项目</span>") {
		t.Fatalf("project navigation must retain its icon and label: %s", primary)
	}
	label := strings.TrimSpace(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(link[1], ""))
	if label != "GitHub 项目" {
		t.Fatalf("project navigation must not include a numeric badge: %s", link[0])
	}
	summary := regexp.MustCompile(`(?s)<section class="library-summary".*?</section>`).FindString(body)
	views := regexp.MustCompile(`(?s)<nav class="library-views".*?</nav>`).FindString(body)
	results := regexp.MustCompile(`(?s)<div class="library-result-meta".*?</div>`).FindString(body)
	if !strings.Contains(summary, "<dd>1,250</dd>") || !strings.Contains(views, "<span>1,250</span>") || !strings.Contains(results, "1,250 个项目") {
		t.Fatal("removing the navigation badge must preserve library summary, tab, and result totals")
	}
}

func TestApprovedAccountEntryUsesRealOAuthAndLogoutCSRF(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	public := html.UnescapeString(fixture.send(fixture.request(http.MethodGet, "/repositories?view=all&new=0&lang=en", nil)).Body.String())
	if !strings.Contains(public, `data-auth-enabled="true"`) || !strings.Contains(public, `/auth/login?`) || !strings.Contains(public, authText(localeEnglish, "login")) {
		t.Fatal("anonymous account entry is not wired to real OAuth")
	}
	for _, private := range []string{`action="/auth/logout`, `data-focus-form`, "@site-admin"} {
		if strings.Contains(public, private) {
			t.Fatalf("anonymous shell exposes owner control %q", private)
		}
	}

	cookie := fixture.login(t)
	owner := html.UnescapeString(fixture.send(fixture.request(http.MethodGet, "/repositories?view=all&new=0&lang=en", nil, cookie)).Body.String())
	logout := regexp.MustCompile(`(?s)<form action="/auth/logout\?lang=en" method="post">.*?name="csrf_token" value="([a-f0-9]{64})".*?</form>`).FindStringSubmatch(owner)
	if !strings.Contains(owner, "@site-admin") || len(logout) != 2 {
		t.Fatal("signed-in account menu lost the real identity or CSRF-protected POST logout")
	}
	for _, forbidden := range []string{"type=\"password\"", "ak_demo_", "sk_demo_", "/api/v1/"} {
		if strings.Contains(owner, forbidden) {
			t.Fatalf("signed-in shell contains demo-only capability %q", forbidden)
		}
	}
}

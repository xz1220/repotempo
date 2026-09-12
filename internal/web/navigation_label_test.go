package web

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

func TestGitHubProjectsNavigationLabelKeepsItsRouteAndLibraryHeading(t *testing.T) {
	primaryNavigation := regexp.MustCompile(`(?s)<nav class="primary-nav"[^>]*>(.*?)</nav>`)
	libraryLink := regexp.MustCompile(`(?s)<a\b[^>]*href="/repositories\?lang=(?:en|zh-CN)"[^>]*>.*?</a>`)
	heading := regexp.MustCompile(`(?s)<h1\b[^>]*>(.*?)</h1>`)
	for _, language := range []struct{ locale, label, title string }{
		{localeChinese, "GitHub 项目", "项目库"},
		{localeEnglish, "GitHub projects", "Project library"},
	} {
		for _, path := range []string{"/", "/repositories", "/repositories/101", "/runs"} {
			t.Run(language.locale+path, func(t *testing.T) {
				response := request(t, newTestHandlerWithLocale(t, populatedFake(), language.locale), path)
				if response.Code != http.StatusOK {
					t.Fatalf("page returned %d", response.Code)
				}
				navigation := primaryNavigation.FindString(response.Body.String())
				link := libraryLink.FindString(navigation)
				if !strings.Contains(link, "<span>"+language.label+"</span>") {
					t.Fatalf("navigation must label /repositories %q; got %s", language.label, link)
				}
				if strings.Contains(navigation, ">项目库<") || strings.Contains(navigation, ">Projects<") {
					t.Fatal("primary navigation retained the old project-library label")
				}
				if path == "/repositories" {
					match := heading.FindStringSubmatch(response.Body.String())
					if len(match) != 2 || strings.TrimSpace(match[1]) != language.title {
						t.Fatalf("the library page heading must remain %q", language.title)
					}
				}
			})
		}
	}
}

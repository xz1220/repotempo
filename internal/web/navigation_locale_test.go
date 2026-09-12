package web

import (
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
)

func TestShellNavigationKeepsSelectedLocale(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		t.Run(locale, func(t *testing.T) {
			// Use the opposite server default so a missing lang cannot pass unnoticed.
			defaultLocale := localeChinese
			if locale == localeChinese {
				defaultLocale = localeEnglish
			}
			handler := newTestHandlerWithLocale(t, populatedFake(), defaultLocale)
			for _, path := range []string{"/", "/repositories", "/repositories/101", "/runs"} {
				body := request(t, handler, path+"?lang="+locale).Body.String()
				for _, pattern := range []string{
					`<a class="brand" href="([^"]+)"`,
					`<a class="nav-link [^"]*" href="([^"]+)"[^>]*>[^<]*<svg[^>]*>.*?</svg><span>`,
				} {
					links := regexp.MustCompile(pattern).FindAllStringSubmatch(body, -1)
					if len(links) == 0 {
						t.Fatalf("%s: missing shell navigation %s", path, pattern)
					}
					for _, match := range links {
						link := navigationAttribute(t, match[0], `href="([^"]+)"`)
						destination, err := url.Parse(link)
						if err != nil || destination.Query().Get("lang") != locale {
							t.Fatalf("%s: navigation lost %s: %s", path, locale, link)
						}
						response := request(t, handler, link)
						if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<html lang="`+locale+`">`) {
							t.Fatalf("%s: destination changed language: %s", path, link)
						}
					}
				}
			}
		})
	}
}

func TestLocalizedLibraryReturnKeepsScopeAndRejectsExternalLinks(t *testing.T) {
	for _, raw := range []string{
		"/repositories?q=audio.cpp&tag=ai&period=7d&page=2&size=6#project-101",
		"/repositories?q=audio.cpp&tag=ai&period=7d&page=2&size=6&lang=zh-CN#project-101",
	} {
		got, err := url.Parse(localizedLibraryReturnURL(raw, localeEnglish))
		if err != nil || got.Query().Get("lang") != localeEnglish || got.Query().Get("q") != "audio.cpp" ||
			got.Query().Get("tag") != "ai" || got.Query().Get("period") != "7d" || got.Query().Get("page") != "2" ||
			got.Query().Get("size") != "6" || got.Fragment != "project-101" {
			t.Fatalf("context lost while setting language: %s", got)
		}
	}
	for _, raw := range []string{"", "//evil.test/repositories", "https://evil.test/repositories", "/repositories?unknown=value"} {
		if got := localizedLibraryReturnURL(raw, localeEnglish); got != "/repositories?lang=en" {
			t.Fatalf("invalid context was not reset to the local library: %s", got)
		}
	}
}

func TestLibraryBreadcrumbsKeepSelectedLocale(t *testing.T) {
	for _, locale := range []string{localeChinese, localeEnglish} {
		defaultLocale := localeChinese
		if locale == localeChinese {
			defaultLocale = localeEnglish
		}
		handler := newTestHandlerWithLocale(t, populatedFake(), defaultLocale)
		for _, path := range []string{"/watch/new", "/repositories/101"} {
			t.Run(locale+path, func(t *testing.T) {
				page := request(t, handler, path+"?lang="+locale)
				if page.Code != http.StatusOK {
					t.Fatalf("page returned %d", page.Code)
				}
				link := navigationAttribute(t, page.Body.String(), `<a class="breadcrumb-link" href="([^"]+)"`)
				destination, err := url.Parse(link)
				if err != nil || destination.Path != "/repositories" || destination.Query().Get("lang") != locale {
					t.Fatalf("breadcrumb lost selected language: %s", link)
				}
				response := request(t, handler, link)
				if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `<html lang="`+locale+`">`) {
					t.Fatalf("breadcrumb destination changed language: %s", link)
				}
			})
		}
	}
}

package auth

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestConfigurationRequiresCompleteExplicitSettings(t *testing.T) {
	for _, mutate := range []func(*Configuration){
		func(c *Configuration) { c.ClientID = "" }, func(c *Configuration) { c.ClientSecret = "" },
		func(c *Configuration) { c.PublicURL = "" }, func(c *Configuration) { c.AllowedUserIDs = nil },
		func(c *Configuration) { c.AllowedUserIDs = []int64{0} }, func(c *Configuration) { c.ClientSecret = "secret\nextra" },
	} {
		config := testConfig()
		mutate(&config)
		if err := config.Validate(); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("partial configuration accepted: %v", err)
		}
	}
	if err := (Configuration{}).Validate(); !errors.Is(err, ErrConfiguration) {
		t.Fatal("all-empty must be handled as disabled by the app, not this service")
	}
}

func TestPublicOriginAllowsHTTPSOrExactLoopbackOnly(t *testing.T) {
	for _, value := range []string{"https://radar.example", "https://radar.example/", "http://localhost:8878", "http://127.0.0.1:8878", "http://[::1]:8878"} {
		config := testConfig()
		config.PublicURL = value
		service, err := New(config, newMemoryStore(), Options{})
		if err != nil || strings.HasSuffix(service.PublicURL(), "/") || service.SecureCookies() != strings.HasPrefix(value, "https://") {
			t.Fatalf("valid origin rejected: %s %v", value, err)
		}
	}
	for _, value := range []string{"http://radar.example", "http://localhost.evil", "http://127.0.0.1.evil", "https://user:password@radar.example", "https://radar.example/path", "https://radar.example?next=evil", "https://radar.example?", "https://radar.example#fragment", "https://radar.example/%2f", "https://radar.example\\@evil", "//radar.example", "https://radar.example:0", "https://radar.example:99999", "https://radar.example:"} {
		config := testConfig()
		config.PublicURL = value
		if err := config.Validate(); !errors.Is(err, ErrConfiguration) {
			t.Fatalf("invalid origin accepted: %s", value)
		}
	}
	for input, expected := range map[string]string{"https://RADAR.example:443/": "https://radar.example", "http://localhost:80": "http://localhost", "http://[::1]:80/": "http://[::1]"} {
		got, err := publicOrigin(input)
		if err != nil || got != expected {
			t.Fatalf("origin is not canonical: %q -> %q %v", input, got, err)
		}
	}
}

func TestSafeReturnPathPreservesFiltersAndOneNestedListReturn(t *testing.T) {
	list := "/repositories?new=0&view=all&sort=day&tag=ai&topic=coding-agents&cursor=opaque%2Bcursor&lang=zh-CN#project-42"
	for _, value := range []string{"/", "/repositories", list, "/repositories/42?return_to=" + url.QueryEscape(list), "/topics/coding-agents", "/runs?limit=20&offset=0", "/watch/new?repository=owner%2Frepo", "/watch/imports/cc789f42-0475-4917-96a2-82de921ee0af?lang=zh-CN"} {
		got, err := SafeReturnPath(value)
		if err != nil || got != value {
			t.Fatalf("lost valid return context: %q => %q %v", value, got, err)
		}
	}
	if got, err := SafeReturnPath(""); err != nil || got != "/repositories" {
		t.Fatal("wrong default return path")
	}
}

func TestSafeReturnPathRejectsRedirectAndEncodedPathAttacks(t *testing.T) {
	for _, value := range []string{
		"https://evil.invalid", "//evil.invalid", "///evil.invalid", "/\\evil.invalid", "/%2f%2fevil.invalid", "/%252f%252fevil.invalid", "/repositories%2f42", "/repositories/../auth/github/callback", "/auth/github/callback", "/static/app.js", "/healthz", "/unknown", "/repositories/0", "/repositories/999999999999999999999999999999", "/repositories?q=%0d%0aLocation%3Aevil", "/repositories?new=0&new=1", "/repositories?redirect=https://evil.invalid", "/repositories#evil", "/repositories?return_to=%2Frepositories", "/repositories/42?return_to=" + url.QueryEscape("https://evil.invalid"), "/repositories/42?return_to=" + url.QueryEscape("%2Frepositories"), "/repositories/42?return_to=" + url.QueryEscape("/repositories?return_to=/repositories"), "/repositories?q=" + strings.Repeat("a", 8192),
	} {
		if _, err := SafeReturnPath(value); !errors.Is(err, ErrInvalidReturn) {
			t.Fatalf("unsafe return accepted: %q", value)
		}
	}
}

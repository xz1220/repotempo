package app

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/xz1220/repotempo/internal/service/auth"
	"github.com/xz1220/repotempo/internal/web"
)

func oauthConfigured(config auth.Configuration) bool {
	return config.ClientID != "" || config.ClientSecret != "" || config.PublicURL != "" || len(config.AllowedUserIDs) > 0 || config.AllowPublicSignup
}

func loadGitHubOAuth() (auth.Configuration, error) {
	config := auth.Configuration{
		ClientID:     strings.TrimSpace(os.Getenv("GITHUB_RADAR_GITHUB_OAUTH_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GITHUB_RADAR_GITHUB_OAUTH_CLIENT_SECRET")),
		PublicURL:    strings.TrimSpace(os.Getenv("GITHUB_RADAR_PUBLIC_URL")),
	}
	switch strings.TrimSpace(os.Getenv("GITHUB_RADAR_PUBLIC_SIGNUP")) {
	case "", "0":
	case "1":
		config.AllowPublicSignup = true
	default:
		return auth.Configuration{}, errors.New("GITHUB_RADAR_PUBLIC_SIGNUP must be 0 or 1")
	}
	rawIDs := strings.TrimSpace(os.Getenv("GITHUB_RADAR_GITHUB_ADMIN_IDS"))
	invalid := errors.New("GitHub login requires GITHUB_RADAR_GITHUB_OAUTH_CLIENT_ID, GITHUB_RADAR_GITHUB_OAUTH_CLIENT_SECRET, GITHUB_RADAR_PUBLIC_URL and positive numeric GITHUB_RADAR_GITHUB_ADMIN_IDS; configure all four or leave all unset")
	if !oauthConfigured(config) && rawIDs == "" {
		return config, nil
	}
	seen := map[int64]bool{}
	for _, raw := range strings.Split(rawIDs, ",") {
		id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil || id <= 0 || seen[id] {
			return auth.Configuration{}, invalid
		}
		seen[id] = true
		config.AllowedUserIDs = append(config.AllowedUserIDs, id)
	}
	if config.Validate() != nil {
		return auth.Configuration{}, invalid
	}
	return config, nil
}

func (runtime *Runtime) webAuthenticator() (web.Authenticator, error) {
	if !oauthConfigured(runtime.settings.GitHubOAuth) {
		return nil, nil
	}
	service, err := auth.New(runtime.settings.GitHubOAuth, runtime.store, auth.Options{Now: runtime.now, HTTPClient: runtime.httpClient})
	if err != nil {
		return nil, errors.New("GitHub login configuration is invalid; management access was not started")
	}
	// The configured owner, never the first public sign-in, receives the legacy
	// private workspace once. Existing data remains intact for CLI operations.
	if err := runtime.store.AdoptLegacyWorkspace(context.Background(), runtime.settings.GitHubOAuth.AllowedUserIDs[0]); err != nil {
		return nil, errors.New("GitHub account workspace setup failed")
	}
	return service, nil
}

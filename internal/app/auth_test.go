package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/xz1220/repotempo/internal/service/auth"
)

var oauthEnvNames = []string{"GITHUB_RADAR_GITHUB_OAUTH_CLIENT_ID", "GITHUB_RADAR_GITHUB_OAUTH_CLIENT_SECRET", "GITHUB_RADAR_PUBLIC_URL", "GITHUB_RADAR_GITHUB_ADMIN_IDS"}

func setOAuthEnv(t *testing.T, values []string) {
	t.Helper()
	for index, key := range oauthEnvNames {
		t.Setenv(key, values[index])
	}
}

func TestOAuthSettingsAreAllOrNothingAndExcludeSecrets(t *testing.T) {
	setOAuthEnv(t, []string{"", "", "", ""})
	settings, err := LoadSettings()
	if err != nil || oauthConfigured(settings.GitHubOAuth) {
		t.Fatalf("disabled config: %v", err)
	}
	complete := []string{"fixture-client-id", "fixture-oauth-secret-not-real", "https://radar.example", "41764150"}
	for missing := range complete {
		values := append([]string(nil), complete...)
		values[missing] = ""
		setOAuthEnv(t, values)
		if _, err := LoadSettings(); err == nil || strings.Contains(err.Error(), complete[1]) {
			t.Fatalf("partial config accepted or secret leaked: %d %v", missing, err)
		}
	}
	setOAuthEnv(t, complete)
	settings, err = LoadSettings()
	if err != nil || !oauthConfigured(settings.GitHubOAuth) || len(settings.GitHubOAuth.AllowedUserIDs) != 1 || settings.GitHubOAuth.AllowedUserIDs[0] != 41764150 {
		t.Fatalf("valid config: %v", err)
	}
	encoded, _ := json.Marshal(settings)
	if strings.Contains(string(encoded), complete[1]) || strings.Contains(fmt.Sprint(settings.DiagnosticFields()), complete[1]) {
		t.Fatal("OAuth client secret leaked through output")
	}
	if settings.DiagnosticFields()["github_login_configured"] != true {
		t.Fatal("configured status missing")
	}
}

func TestOAuthAdminIDsMustBeExplicitAndPositive(t *testing.T) {
	for _, ids := range []string{"xz1220", "0", "-1", "41764150,41764150", "41764150,", ",42", "1.5"} {
		setOAuthEnv(t, []string{"fixture-client", "fixture-secret-not-real", "https://radar.example", ids})
		if _, err := LoadSettings(); err == nil {
			t.Fatalf("invalid administrators accepted: %s", ids)
		}
	}
	setOAuthEnv(t, []string{"fixture-client", "fixture-secret-not-real", "http://remote.example", "42"})
	if _, err := LoadSettings(); err == nil {
		t.Fatal("remote HTTP login accepted")
	}
	setOAuthEnv(t, []string{"fixture-client", "fixture-secret-not-real", "http://127.0.0.1:8878", "42"})
	if _, err := LoadSettings(); err != nil {
		t.Fatalf("loopback development rejected: %v", err)
	}
}

func TestPublicSignupConfigurationIsExplicitAndRequiresAdminOwner(t *testing.T) {
	setOAuthEnv(t, []string{"fixture-client", "fixture-secret-not-real", "https://radar.example", "42"})
	t.Setenv("GITHUB_RADAR_PUBLIC_SIGNUP", "1")
	settings, err := LoadSettings()
	if err != nil || !settings.GitHubOAuth.AllowPublicSignup {
		t.Fatalf("public signup: %v", err)
	}
	t.Setenv("GITHUB_RADAR_GITHUB_ADMIN_IDS", "")
	if _, err := LoadSettings(); err == nil {
		t.Fatal("public signup accepted without explicitly configured legacy admin owner")
	}
	t.Setenv("GITHUB_RADAR_PUBLIC_SIGNUP", "true")
	if _, err := LoadSettings(); err == nil {
		t.Fatal("invalid signup value accepted")
	}
}

func TestWebAuthenticatorNilIsReallyNilAndPartialConfigFailsClosed(t *testing.T) {
	runtime, err := OpenRuntime(context.Background(), Settings{DatabasePath: t.TempDir() + "/auth.db"})
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	service, err := runtime.webAuthenticator()
	if err != nil || service != nil {
		t.Fatalf("disabled OAuth created a typed-nil authenticator: %v", err)
	}
	runtime.settings.GitHubOAuth = auth.Configuration{ClientID: "missing-other-settings"}
	if _, err := runtime.webAuthenticator(); err == nil {
		t.Fatal("partial runtime config fell back to legacy management access")
	}
	runtime.settings.GitHubOAuth = auth.Configuration{ClientID: "fixture-id", ClientSecret: "fixture-secret", PublicURL: "https://radar.example", AllowedUserIDs: []int64{42}}
	service, err = runtime.webAuthenticator()
	if err != nil || service == nil || service.PublicURL() != "https://radar.example" {
		t.Fatalf("valid authenticator unavailable: %v", err)
	}
}

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

type githubProvider struct {
	oauth  oauth2.Config
	client *http.Client
}

func newGitHubProvider(config oauth2.Config, supplied *http.Client) *githubProvider {
	client := &http.Client{}
	if supplied != nil {
		*client = *supplied
	}
	if client.Timeout <= 0 || client.Timeout > 12*time.Second {
		client.Timeout = 12 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	transport := client.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	client.Transport = providerTransport{base: transport}
	return &githubProvider{oauth: config, client: client}
}

func (provider *githubProvider) ExchangeIdentity(ctx context.Context, code, verifier string) (Identity, error) {
	// PKCE exchange is handled by the maintained OAuth library. Neither the
	// access token nor a possible refresh token leaves this function.
	exchangeContext := context.WithValue(ctx, oauth2.HTTPClient, provider.client)
	token, err := provider.oauth.Exchange(exchangeContext, code, oauth2.VerifierOption(verifier))
	if err != nil || token.AccessToken == "" || !strings.EqualFold(token.Type(), "Bearer") {
		return Identity{}, ErrProvider
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil {
		return Identity{}, ErrProvider
	}
	request.Header.Set("Authorization", "Bearer "+token.AccessToken)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "RepoTempo-auth")
	response, err := provider.client.Do(request)
	if err != nil {
		return Identity{}, ErrProvider
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Identity{}, ErrProvider
	}
	var payload struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
	}
	decoder := json.NewDecoder(response.Body)
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF || payload.ID <= 0 || !validLogin.MatchString(payload.Login) {
		return Identity{}, ErrProvider
	}
	return Identity{ID: payload.ID, Login: payload.Login}, nil
}

// Restrict both requests before any credentials are sent, and cap even error
// responses before the OAuth library decodes them. No redirects are followed.
type providerTransport struct{ base http.RoundTripper }

func (transport providerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	url := request.URL
	if url.Scheme != "https" || url.User != nil || url.RawPath != "" || url.RawQuery != "" || url.ForceQuery || url.Fragment != "" ||
		!(request.Method == http.MethodPost && url.Host == "github.com" && url.Path == "/login/oauth/access_token" || request.Method == http.MethodGet && url.Host == "api.github.com" && url.Path == "/user") {
		return nil, ErrProvider
	}
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, ErrProvider
	}
	if response == nil || response.Body == nil {
		return nil, ErrProvider
	}
	const limit = 64 * 1024
	body, readErr := io.ReadAll(io.LimitReader(response.Body, limit+1))
	_ = response.Body.Close()
	if readErr != nil || len(body) > limit {
		return nil, ErrProvider
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	return response, nil
}

package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SleepFunc func(context.Context, time.Duration) error

type ClientOptions struct {
	BaseURL          string
	Token            string
	APIVersion       string
	UserAgent        string
	HTTPClient       *http.Client
	Timeout          time.Duration
	CoreInterval     time.Duration
	SearchInterval   time.Duration
	SecondaryBackoff time.Duration
	ServerBackoff    time.Duration
	MaxRetries       int
	Now              func() time.Time
	Sleep            SleepFunc
}

type Client struct {
	baseURL          *url.URL
	token            string
	apiVersion       string
	userAgent        string
	httpClient       *http.Client
	coreInterval     time.Duration
	searchInterval   time.Duration
	secondaryBackoff time.Duration
	serverBackoff    time.Duration
	maxRetries       int
	now              func() time.Time
	sleep            SleepFunc

	requestMu   sync.Mutex
	lastRequest map[Resource]time.Time
	rateMu      sync.RWMutex
	rates       map[Resource]RateLimit
}

type rawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	URL        *url.URL
	RateLimit  RateLimit
}

func NewClient(options ClientOptions) (*Client, error) {
	baseURL, err := url.Parse(options.BaseURL)
	if err != nil || baseURL.Scheme == "" || baseURL.Host == "" {
		return nil, fmt.Errorf("invalid GitHub API base URL %q", options.BaseURL)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, errors.New("GitHub API base URL must use HTTP or HTTPS")
	}
	if options.UserAgent == "" {
		return nil, errors.New("GitHub user agent is required")
	}
	if options.MaxRetries < 0 {
		return nil, errors.New("GitHub max retries must not be negative")
	}
	if options.MaxRetries > 5 {
		return nil, errors.New("GitHub max retries must not exceed five")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/"

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	copyClient := *httpClient
	copyClient.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	if options.Timeout > 0 {
		copyClient.Timeout = options.Timeout
	} else if copyClient.Timeout == 0 {
		copyClient.Timeout = 30 * time.Second
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	if options.SecondaryBackoff == 0 {
		options.SecondaryBackoff = time.Minute
	}
	if options.SecondaryBackoff < time.Minute {
		return nil, errors.New("GitHub secondary backoff must be at least one minute")
	}
	if options.ServerBackoff == 0 {
		options.ServerBackoff = time.Second
	}

	return &Client{
		baseURL:          baseURL,
		token:            options.Token,
		apiVersion:       options.APIVersion,
		userAgent:        options.UserAgent,
		httpClient:       &copyClient,
		coreInterval:     options.CoreInterval,
		searchInterval:   options.SearchInterval,
		secondaryBackoff: options.SecondaryBackoff,
		serverBackoff:    options.ServerBackoff,
		maxRetries:       options.MaxRetries,
		now:              options.Now,
		sleep:            options.Sleep,
		lastRequest:      make(map[Resource]time.Time),
		rates:            make(map[Resource]RateLimit),
	}, nil
}

func (c *Client) RateLimits() map[Resource]RateLimit {
	c.rateMu.RLock()
	defer c.rateMu.RUnlock()
	result := make(map[Resource]RateLimit, len(c.rates))
	for resource, value := range c.rates {
		result[resource] = value
	}
	return result
}

func (c *Client) resolve(relative string) (*url.URL, error) {
	reference, err := url.Parse(relative)
	if err != nil {
		return nil, err
	}
	return c.baseURL.ResolveReference(reference), nil
}

func (c *Client) do(ctx context.Context, target *url.URL, resource Resource, headers http.Header) (rawResponse, error) {
	if target.Scheme != c.baseURL.Scheme || !strings.EqualFold(target.Host, c.baseURL.Host) {
		return rawResponse{}, errors.New("refusing to send GitHub credentials to a different origin")
	}

	// Holding the lock across waiting, retries, and transport calls makes the
	// client a serial queue, as GitHub recommends for avoiding secondary limits.
	c.requestMu.Lock()
	defer c.requestMu.Unlock()

	for attempt := 0; ; attempt++ {
		if err := c.waitBeforeRequest(ctx, resource); err != nil {
			return rawResponse{}, classifyTransportError(err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
		if err != nil {
			return rawResponse{}, err
		}
		request.Header.Set("Accept", "application/vnd.github+json")
		request.Header.Set("User-Agent", c.userAgent)
		if c.apiVersion != "" {
			request.Header.Set("X-GitHub-Api-Version", c.apiVersion)
		}
		if c.token != "" {
			request.Header.Set("Authorization", "Bearer "+c.token)
		}
		for key, values := range headers {
			for _, value := range values {
				request.Header.Add(key, value)
			}
		}

		c.lastRequest[resource] = c.now()
		response, err := c.httpClient.Do(request)
		if err != nil {
			return rawResponse{}, classifyTransportError(err)
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, 32<<20))
		closeErr := response.Body.Close()
		if readErr != nil {
			return rawResponse{}, classifyTransportError(readErr)
		}
		if closeErr != nil {
			return rawResponse{}, classifyTransportError(closeErr)
		}
		rate := parseRateLimit(response.Header, resource, c.now())
		c.recordRate(rate)
		raw := rawResponse{
			StatusCode: response.StatusCode,
			Header:     response.Header.Clone(),
			Body:       body,
			URL:        request.URL,
			RateLimit:  rate,
		}
		delay, retry := c.retryDelay(raw, attempt)
		if !retry || attempt >= c.maxRetries {
			return raw, nil
		}
		if err := c.sleep(ctx, delay); err != nil {
			return rawResponse{}, classifyTransportError(err)
		}
	}
}

func (c *Client) waitBeforeRequest(ctx context.Context, resource Resource) error {
	now := c.now()
	if rate, ok := c.rate(resource); ok && rate.Remaining == 0 && rate.Reset.After(now) {
		if err := c.sleep(ctx, rate.Reset.Sub(now)+time.Second); err != nil {
			return err
		}
		now = c.now()
	}
	interval := c.coreInterval
	if resource == ResourceSearch {
		interval = c.searchInterval
	}
	if last := c.lastRequest[resource]; interval > 0 && !last.IsZero() {
		if remaining := interval - now.Sub(last); remaining > 0 {
			return c.sleep(ctx, remaining)
		}
	}
	return nil
}

func (c *Client) retryDelay(response rawResponse, attempt int) (time.Duration, bool) {
	status := response.StatusCode
	if status != http.StatusForbidden && status != http.StatusTooManyRequests && status < 500 {
		return 0, false
	}
	if response.RateLimit.RetryAfter > 0 {
		return response.RateLimit.RetryAfter, true
	}
	if response.RateLimit.Remaining == 0 && response.RateLimit.Reset.After(c.now()) {
		return response.RateLimit.Reset.Sub(c.now()) + time.Second, true
	}
	if status == http.StatusForbidden || status == http.StatusTooManyRequests {
		if isSecondaryLimit(response.Body) || status == http.StatusTooManyRequests {
			return exponential(c.secondaryBackoff, attempt), true
		}
		return 0, false
	}
	return exponential(c.serverBackoff, attempt), true
}

func exponential(base time.Duration, attempt int) time.Duration {
	if attempt > 20 {
		attempt = 20
	}
	return base * time.Duration(1<<attempt)
}

func (c *Client) rate(resource Resource) (RateLimit, bool) {
	c.rateMu.RLock()
	defer c.rateMu.RUnlock()
	value, ok := c.rates[resource]
	return value, ok
}

func (c *Client) recordRate(rate RateLimit) {
	if rate.Resource == "" {
		return
	}
	c.rateMu.Lock()
	c.rates[rate.Resource] = rate
	c.rateMu.Unlock()
}

func parseRateLimit(header http.Header, fallback Resource, now time.Time) RateLimit {
	resource := Resource(header.Get("X-RateLimit-Resource"))
	if resource == "" {
		resource = fallback
	}
	remaining := -1
	if raw := header.Get("X-RateLimit-Remaining"); raw != "" {
		if value, err := strconv.Atoi(raw); err == nil {
			remaining = value
		}
	}
	rate := RateLimit{
		Resource:  resource,
		Limit:     parseHeaderInt(header, "X-RateLimit-Limit"),
		Remaining: remaining,
		Used:      parseHeaderInt(header, "X-RateLimit-Used"),
	}
	if raw := header.Get("X-RateLimit-Reset"); raw != "" {
		if epoch, err := strconv.ParseInt(raw, 10, 64); err == nil {
			rate.Reset = time.Unix(epoch, 0).UTC()
		}
	}
	rate.RetryAfter = parseRetryAfter(header.Get("Retry-After"), now)
	return rate
}

func parseHeaderInt(header http.Header, name string) int {
	value, _ := strconv.Atoi(header.Get(name))
	return value
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	if raw == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil {
		if seconds <= 0 {
			return 0
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil || !when.After(now) {
		return 0
	}
	return when.Sub(now)
}

func isSecondaryLimit(body []byte) bool {
	var payload struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(body, &payload)
	message := strings.ToLower(payload.Message)
	return strings.Contains(message, "secondary rate limit") || strings.Contains(message, "abuse detection")
}

func classifyTransportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &TransportError{Code: CodeTimeout, Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &TransportError{Code: CodeCanceled, Err: err}
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return &TransportError{Code: CodeTimeout, Err: err}
	}
	return &TransportError{Code: CodeTransport, Err: err}
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	if duration <= 0 {
		return nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func apiError(response rawResponse) error {
	message := ""
	var payload struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(response.Body, &payload) == nil {
		message = payload.Message
	}
	code := CodeBadRequest
	switch response.StatusCode {
	case http.StatusNotModified:
		code = CodeNotModified
	case http.StatusForbidden:
		if response.RateLimit.Remaining == 0 {
			code = CodePrimaryRateLimit
		} else if isSecondaryLimit(response.Body) {
			code = CodeSecondaryRateLimit
		} else {
			code = CodeForbidden
		}
	case http.StatusNotFound:
		code = CodeNotFoundOrPrivate
	case http.StatusUnprocessableEntity:
		code = CodeValidation
	case http.StatusTooManyRequests:
		if response.RateLimit.Remaining == 0 {
			code = CodePrimaryRateLimit
		} else {
			code = CodeSecondaryRateLimit
		}
	default:
		if response.StatusCode >= 500 {
			code = CodeUpstream
		}
	}
	path := ""
	if response.URL != nil {
		path = response.URL.EscapedPath()
		if response.URL.RawQuery != "" {
			path += "?" + response.URL.RawQuery
		}
	}
	return &APIError{Code: code, StatusCode: response.StatusCode, Message: message, Path: path, RateLimit: response.RateLimit}
}

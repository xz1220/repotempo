package trending

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const maxResponseBytes = 4 << 20

type ClientOptions struct {
	BaseURL    string
	HTTPClient *http.Client
	Timeout    time.Duration
	Interval   time.Duration
	UserAgent  string
	Now        func() time.Time
}

type Client struct {
	baseURL       *url.URL
	httpClient    *http.Client
	interval      time.Duration
	userAgent     string
	now           func() time.Time
	queue         chan struct{}
	lastMu        sync.Mutex
	lastFetch     time.Time
	deferredUntil time.Time
}

func NewClient(options ClientOptions) (*Client, error) {
	if options.BaseURL == "" {
		options.BaseURL = "https://github.com/"
	}
	base, err := url.Parse(options.BaseURL)
	if err != nil || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Path != "" && base.Path != "/") {
		return nil, errors.New("trending base URL must be an origin without credentials, path, query, or fragment")
	}
	loopback := false
	if ip := net.ParseIP(base.Hostname()); ip != nil {
		loopback = ip.IsLoopback()
	}
	if strings.EqualFold(base.Hostname(), "localhost") {
		loopback = true
	}
	official := base.Scheme == "https" && strings.EqualFold(base.Hostname(), "github.com") && (base.Port() == "" || base.Port() == "443")
	if !official && !(loopback && (base.Scheme == "http" || base.Scheme == "https")) {
		return nil, errors.New("trending only supports the official GitHub origin or a loopback test server")
	}
	if options.Timeout < 0 || options.Interval < 0 {
		return nil, errors.New("trending timeout and interval must not be negative")
	}
	if options.Timeout == 0 {
		options.Timeout = 20 * time.Second
	}
	if options.Interval == 0 {
		options.Interval = 2 * time.Second
	}
	if official && options.Interval < time.Second {
		options.Interval = time.Second
	}
	if options.UserAgent == "" {
		options.UserAgent = "repotempo/0.2"
	}
	if strings.ContainsAny(options.UserAgent, "\r\n") {
		return nil, errors.New("trending user agent contains a newline")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	client := http.Client{}
	if options.HTTPClient != nil {
		client = *options.HTTPClient
	}
	client.Timeout = options.Timeout
	// Trending is public. Do not inherit browser/account session cookies.
	client.Jar = nil
	client.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if !sameOrigin(base, next.URL) {
			return errors.New("trending refused a cross-origin redirect")
		}
		if len(via) >= 3 {
			return errors.New("trending exceeded the redirect limit")
		}
		if next.URL.Path != "/trending" || next.URL.RawQuery != via[0].URL.RawQuery || next.URL.User != nil || next.URL.Fragment != "" {
			return errors.New("trending refused a redirect outside the requested all-language list")
		}
		return nil
	}
	return &Client{baseURL: base, httpClient: &client, interval: options.Interval, userAgent: options.UserAgent, now: options.Now, queue: make(chan struct{}, 1)}, nil
}

// FetchWindows fetches each requested list once, in order. A failed window is
// included with Error, and later windows are still attempted. Parsing failures
// retain usable entries as partial evidence but never report complete success.
func (c *Client) FetchWindows(ctx context.Context, periods []string) (Result, error) {
	result := Result{Windows: []WindowResult{}}
	if len(periods) == 0 {
		return result, errors.New("trending requires at least one period")
	}
	seen := map[string]bool{}
	for _, period := range periods {
		if !validPeriod(period) {
			return result, fmt.Errorf("unsupported trending period %q", period)
		}
		if seen[period] {
			return result, fmt.Errorf("duplicate trending period %q", period)
		}
		seen[period] = true
	}
	// Serialize calls to the same client, including their pacing. Waiting for
	// the queue is cancellable, so no concurrent request bypasses the interval.
	select {
	case c.queue <- struct{}{}:
		defer func() { <-c.queue }()
	case <-ctx.Done():
		return result, ctx.Err()
	}
	var failures []error
	for _, period := range periods {
		target := *c.baseURL
		target.Path = "/trending"
		target.RawQuery = url.Values{"since": []string{period}}.Encode()
		window := WindowResult{Period: period, URL: target.String(), Entries: []Entry{}}
		entries, err := c.fetch(ctx, &target, period)
		window.CapturedAt = c.now().UTC()
		window.Entries = entries
		if err != nil {
			window.Error = err.Error()
			failures = append(failures, fmt.Errorf("trending %s: %w", period, err))
		}
		result.Windows = append(result.Windows, window)
	}
	return result, errors.Join(failures...)
}

func (c *Client) fetch(ctx context.Context, target *url.URL, period string) ([]Entry, error) {
	empty := []Entry{}
	if c.now().Before(c.deferredUntil) {
		return empty, fmt.Errorf("request deferred by GitHub Retry-After until %s", c.deferredUntil.UTC().Format(time.RFC3339))
	}
	if err := c.wait(ctx); err != nil {
		return empty, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return empty, err
	}
	request.Header.Set("Accept", "text/html")
	request.Header.Set("Accept-Language", "en")
	request.Header.Set("User-Agent", c.userAgent)
	c.lastMu.Lock()
	c.lastFetch = time.Now()
	c.lastMu.Unlock()
	response, err := c.httpClient.Do(request)
	if err != nil {
		return empty, fmt.Errorf("request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		// No retries or alternate identities on forbidden/rate-limited pages.
		if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
			c.deferredUntil = retryAfter(response.Header.Get("Retry-After"), c.now())
		}
		return empty, fmt.Errorf("HTTP %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}
	if contentType := response.Header.Get("Content-Type"); contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
			return empty, errors.New("response is not an HTML page")
		}
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil {
		return empty, fmt.Errorf("read HTML response: %w", err)
	}
	if len(body) > maxResponseBytes {
		return empty, errors.New("HTML response exceeds 4 MiB limit")
	}
	if !utf8.Valid(body) {
		return empty, errors.New("HTML response is not valid UTF-8")
	}
	return parsePage(body, target, period)
}

func retryAfter(value string, now time.Time) time.Time {
	if seconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil && seconds > 0 {
		// Keep an abusive header from overflowing time.Duration.
		if seconds > 86400 {
			seconds = 86400
		}
		return now.Add(time.Duration(seconds) * time.Second)
	}
	if deadline, err := http.ParseTime(value); err == nil {
		return deadline
	}
	return time.Time{}
}

func (c *Client) wait(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.lastMu.Lock()
	remaining := time.Until(c.lastFetch.Add(c.interval))
	c.lastMu.Unlock()
	if remaining <= 0 {
		return nil
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validPeriod(period string) bool {
	return period == "daily" || period == "weekly" || period == "monthly"
}

func sameOrigin(left, right *url.URL) bool {
	port := func(value *url.URL) string {
		if value.Port() != "" {
			return value.Port()
		}
		if value.Scheme == "https" {
			return "443"
		}
		return "80"
	}
	return left.Scheme == right.Scheme && strings.EqualFold(left.Hostname(), right.Hostname()) && port(left) == port(right)
}

package web

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	loginauth "github.com/xz1220/repotempo/internal/service/auth"
)

func authLimitedRequest(remote string, forwarded ...string) *http.Request {
	request := &http.Request{RemoteAddr: remote, Header: make(http.Header)}
	for _, value := range forwarded {
		request.Header.Add("X-Forwarded-For", value)
	}
	return request
}

func TestAuthStartLimitPerIPAndExpiry(t *testing.T) {
	var limiter authStartLimiter
	now := time.Date(2026, 9, 10, 10, 0, 0, 0, time.UTC)
	request := authLimitedRequest("198.51.100.1:2345")
	for index := 0; index < authStartLimit; index++ {
		if !limiter.allow(request, now) {
			t.Fatalf("denied allowed attempt %d", index+1)
		}
	}
	if limiter.allow(request, now) || limiter.allow(request, now.Add(authStartWindow-time.Nanosecond)) {
		t.Fatal("allowed more than ten login starts inside one window")
	}
	if !limiter.allow(authLimitedRequest("198.51.100.2:2345"), now) {
		t.Fatal("another IP shared the exhausted limit")
	}
	if !limiter.allow(request, now.Add(authStartWindow)) {
		t.Fatal("limit did not reset at exact expiration")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("expired buckets not cleaned: %d", len(limiter.buckets))
	}
	if bucket := limiter.buckets[netip.MustParseAddr("198.51.100.1")]; bucket.count != 1 || !bucket.expires.Equal(now.Add(2*authStartWindow)) {
		t.Fatalf("new window=%+v", bucket)
	}
}

func TestAuthStartIPTrustsOnlyLoopbackProxyFinalAddress(t *testing.T) {
	for _, fixture := range []struct {
		name, remote string
		forwarded    []string
		want         string
	}{
		{"direct ignores spoofed chain", "198.51.100.1:2345", []string{"1.1.1.1, 203.0.113.9"}, "198.51.100.1"},
		{"direct ignores malformed header", "198.51.100.1:2345", []string{"garbage"}, "198.51.100.1"},
		{"proxy chooses final item", "127.0.0.1:2345", []string{"1.1.1.1, 2.2.2.2, 198.51.100.1"}, "198.51.100.1"},
		{"proxy ignores forged prefix", "127.0.0.1:2345", []string{"unknown, ", "forged.example, 198.51.100.1"}, "198.51.100.1"},
		{"ipv6 proxy", "[::1]:2345", []string{"192.0.2.2, 2001:db8::abcd"}, "2001:db8::abcd"},
		{"mapped proxy and mapped client", "[::ffff:127.0.0.1]:2345", []string{"::ffff:198.51.100.1"}, "198.51.100.1"},
		{"ipv6 normalized", "[2001:db8:0:0:0:0:0:1]:2345", nil, "2001:db8::1"},
		{"local without proxy", "127.0.0.1:2345", nil, "127.0.0.1"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			got, ok := authStartIP(authLimitedRequest(fixture.remote, fixture.forwarded...))
			if !ok || got.String() != fixture.want {
				t.Fatalf("IP=%s ok=%v want=%s", got, ok, fixture.want)
			}
		})
	}
}

func TestAuthStartForwardedPrefixCannotResetQuota(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	for index := 0; index < authStartLimit; index++ {
		request := authLimitedRequest("127.0.0.1:2345", fmt.Sprintf("203.0.113.%d, 198.51.100.8", index))
		if !limiter.allow(request, now) {
			t.Fatal("valid proxied attempt refused")
		}
	}
	if limiter.allow(authLimitedRequest("127.0.0.1:6789", "192.0.2.99, 198.51.100.8"), now) {
		t.Fatal("forged prefix or new TCP port reset the quota")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("spoofed prefixes became bucket keys: %d", len(limiter.buckets))
	}
	if !limiter.allow(authLimitedRequest("127.0.0.1:2345", "198.51.100.9"), now) {
		t.Fatal("real clients behind nginx shared a single bucket")
	}
}

func TestAuthStartDirectSpoofedHeadersAndIPv4AliasesShareRealIPQuota(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	for index := 0; index < authStartLimit; index++ {
		remote := "198.51.100.8:2345"
		if index%2 == 1 {
			remote = "[::ffff:198.51.100.8]:6789"
		}
		request := authLimitedRequest(remote, fmt.Sprintf("203.0.113.%d", index))
		request.Header.Set("X-Real-IP", fmt.Sprintf("192.0.2.%d", index))
		request.Header.Set("Forwarded", fmt.Sprintf("for=192.0.2.%d", index))
		if !limiter.allow(request, now) {
			t.Fatal("valid direct attempt refused")
		}
	}
	if limiter.allow(authLimitedRequest("198.51.100.8:9000", "192.0.2.200"), now) {
		t.Fatal("forged proxy header or IPv4-mapped alias reset real IP quota")
	}
	if len(limiter.buckets) != 1 {
		t.Fatalf("direct attacker minted %d keys", len(limiter.buckets))
	}
}

func TestAuthStartInvalidAddressesFailClosedWithoutAllocatingKeys(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	for _, request := range []*http.Request{
		nil,
		authLimitedRequest(""),
		authLimitedRequest("attacker.example:443", "198.51.100.1"),
		authLimitedRequest("198.51.100.1"),
		authLimitedRequest("198.51.100.1:70000"),
		authLimitedRequest("[fe80::1%interface]:2345"),
		authLimitedRequest("127.0.0.1:2345", ""),
		authLimitedRequest("127.0.0.1:2345", "198.51.100.1,"),
		authLimitedRequest("127.0.0.1:2345", "198.51.100.1:80"),
		authLimitedRequest("127.0.0.1:2345", "198.51.100.1, attacker.example"),
		authLimitedRequest("127.0.0.1:2345", "[::1]"),
		authLimitedRequest("127.0.0.1:2345", "fe80::1%interface"),
	} {
		if limiter.allow(request, now) {
			t.Fatal("invalid address was allowed")
		}
	}
	if limiter.allow(authLimitedRequest("198.51.100.1:2345"), time.Time{}) {
		t.Fatal("missing clock value was accepted")
	}
	if len(limiter.buckets) != 0 {
		t.Fatalf("invalid requests allocated %d keys", len(limiter.buckets))
	}
}

func TestAuthStartMapIsBoundedAndDoesNotEvictLiveBuckets(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	for index := 0; index < authStartMaxIPs; index++ {
		request := authLimitedRequest(fmt.Sprintf("[2001:db8::%x]:2345", index+1))
		if !limiter.allow(request, now) {
			t.Fatalf("refused allowed bucket %d", index)
		}
	}
	known := authLimitedRequest("[2001:db8::1]:6789")
	for index := 1; index < authStartLimit; index++ {
		if !limiter.allow(known, now) {
			t.Fatal("full map blocked an existing IP with allowance")
		}
	}
	for index := authStartMaxIPs; index < authStartMaxIPs+50; index++ {
		if limiter.allow(authLimitedRequest(fmt.Sprintf("[2001:db8::%x]:2345", index+1)), now) {
			t.Fatal("live bucket map grew beyond capacity")
		}
	}
	if len(limiter.buckets) != authStartMaxIPs || limiter.allow(known, now) {
		t.Fatal("IP churn evicted a live bucket or reset quota")
	}
	if !limiter.allow(authLimitedRequest("203.0.113.1:2345"), now.Add(authStartWindow)) || len(limiter.buckets) != 1 {
		t.Fatal("expired full map did not reclaim capacity")
	}
}

func TestAuthStartConcurrentSameIPAllowsExactlyTen(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	var allowed atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < 100; index++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if limiter.allow(authLimitedRequest("198.51.100.1:2345"), now) {
				allowed.Add(1)
			}
		}()
	}
	group.Wait()
	if allowed.Load() != authStartLimit || len(limiter.buckets) != 1 {
		t.Fatalf("concurrent allowance=%d buckets=%d", allowed.Load(), len(limiter.buckets))
	}
}

func TestAuthStartConcurrentNewIPsStayWithinMapBound(t *testing.T) {
	var limiter authStartLimiter
	now := time.Now()
	var allowed atomic.Int32
	var group sync.WaitGroup
	for index := 0; index < authStartMaxIPs+64; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			request := authLimitedRequest(fmt.Sprintf("[2001:db8::%x]:2345", index+1))
			if limiter.allow(request, now) {
				allowed.Add(1)
			}
		}(index)
	}
	group.Wait()
	if allowed.Load() != authStartMaxIPs || len(limiter.buckets) != authStartMaxIPs {
		t.Fatalf("concurrent capacity=%d buckets=%d", allowed.Load(), len(limiter.buckets))
	}
}

type authLimitCountingAuthenticator struct {
	Authenticator
	starts int
}

func (auth *authLimitCountingAuthenticator) Begin(ctx context.Context, path string) (loginauth.LoginStart, error) {
	auth.starts++
	return auth.Authenticator.Begin(ctx, path)
}

func TestAuthStartHTTPRejectsBeforeCreatingStateAndRecoversAfterWindow(t *testing.T) {
	fixture := newAuthHTTPFixture(t)
	auth := &authLimitCountingAuthenticator{Authenticator: fixture.service}
	fixture.handler.auth = auth
	for index := 0; index < authStartLimit; index++ {
		response := fixture.send(fixture.request(http.MethodGet, "/auth/github/start", nil))
		if response.Code != http.StatusSeeOther {
			t.Fatalf("allowed login start %d: status=%d", index+1, response.Code)
		}
	}
	for index := 0; index < 25; index++ {
		request := fixture.request(http.MethodGet, "/auth/github/start", nil)
		request.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", index))
		response := fixture.send(request)
		if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") != "600" || response.Header().Get("Location") != "" {
			t.Fatalf("limit HTTP result: status=%d headers=%v", response.Code, response.Header())
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == fixture.handler.bindingCookieName() {
				t.Fatal("rate-limited start minted a new browser binding")
			}
		}
	}
	if auth.starts != authStartLimit {
		t.Fatalf("denied requests still created OAuth states: %d starts", auth.starts)
	}
	request := fixture.request(http.MethodGet, "/auth/github/start", nil)
	request.RemoteAddr = "192.0.2.6:4567"
	if response := fixture.send(request); response.Code != http.StatusSeeOther {
		t.Fatal("another real IP was incorrectly blocked")
	}
	fixture.now = fixture.now.Add(authStartWindow)
	if response := fixture.send(fixture.request(http.MethodGet, "/auth/github/start", nil)); response.Code != http.StatusSeeOther {
		t.Fatalf("expired limiter did not permit a fresh OAuth flow: %d", response.Code)
	}
	if auth.starts != authStartLimit+2 {
		t.Fatalf("unexpected state creation count: %d", auth.starts)
	}
}

package web

import (
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	authStartLimit  = 10
	authStartWindow = 10 * time.Minute
	authStartMaxIPs = 1024
)

type authStartBucket struct {
	count   int
	expires time.Time
}

// authStartLimiter protects the bounded OAuth-state table before state creation.
// The zero value is ready for use. Buckets are process-local: restarting the
// process resets the limit; the persistent OAuth-state cap remains a backstop.
type authStartLimiter struct {
	mu      sync.Mutex
	buckets map[netip.Addr]authStartBucket
}

func (limiter *authStartLimiter) allow(request *http.Request, now time.Time) bool {
	ip, ok := authStartIP(request)
	if !ok || now.IsZero() {
		return false
	}
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	if limiter.buckets == nil {
		limiter.buckets = make(map[netip.Addr]authStartBucket)
	}
	// The fixed bound also limits cleanup work. Never evict an active bucket
	// merely to admit a new IP: doing that lets churn reset an attacker's quota.
	for key, bucket := range limiter.buckets {
		if !bucket.expires.After(now) {
			delete(limiter.buckets, key)
		}
	}
	bucket, exists := limiter.buckets[ip]
	if !exists {
		if len(limiter.buckets) >= authStartMaxIPs {
			return false
		}
		bucket.expires = now.Add(authStartWindow)
	}
	if bucket.count >= authStartLimit {
		return false
	}
	bucket.count++
	limiter.buckets[ip] = bucket
	return true
}

// The production nginx appends its immediate client address using
// proxy_add_x_forwarded_for. Only a loopback connection can assert that header,
// and only its final item is trusted; attacker-controlled prefixes are ignored.
func authStartIP(request *http.Request) (netip.Addr, bool) {
	if request == nil {
		return netip.Addr{}, false
	}
	remote, err := netip.ParseAddrPort(request.RemoteAddr)
	if err != nil || remote.Addr().Zone() != "" {
		return netip.Addr{}, false
	}
	ip := remote.Addr().Unmap()
	if !ip.IsLoopback() {
		return ip, true
	}
	forwarded := request.Header.Values("X-Forwarded-For")
	if len(forwarded) == 0 {
		return ip, true
	}
	last := forwarded[len(forwarded)-1]
	last = strings.TrimSpace(last[strings.LastIndexByte(last, ',')+1:])
	forwardedIP, err := netip.ParseAddr(last)
	if err != nil || forwardedIP.Zone() != "" {
		// Invalid proxy input is not a new bucket key or a way to switch to a
		// shared fallback bucket after using up the real address's allowance.
		return netip.Addr{}, false
	}
	return forwardedIP.Unmap(), true
}

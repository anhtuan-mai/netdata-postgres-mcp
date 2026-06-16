// SPDX-License-Identifier: GPL-3.0-or-later

// Package middleware provides HTTP middleware for the MCP server.
package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/metrics"
)

// RateLimiter implements a per-IP token bucket rate limiter.
// Zero-dependency — no external libraries required.
type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*bucket
	rate     float64 // tokens per second
	burst    int     // max tokens
	cleanup  time.Duration
}

type bucket struct {
	tokens   float64
	lastSeen time.Time
}

// NewRateLimiter creates a rate limiter that allows `rate` requests per second
// with a burst capacity of `burst`. Stale entries are cleaned up every minute.
func NewRateLimiter(rate float64, burst int) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*bucket),
		rate:     rate,
		burst:    burst,
		cleanup:  3 * time.Minute,
	}
	go rl.cleanupLoop()
	return rl
}

// Allow checks if a request from the given IP is allowed.
func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	b, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &bucket{
			tokens:   float64(rl.burst) - 1,
			lastSeen: now,
		}
		return true
	}

	// Refill tokens based on elapsed time
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * rl.rate
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastSeen = now

	if b.tokens >= 1 {
		b.tokens--
		return true
	}

	return false
}

// Handler wraps an http.Handler with rate limiting.
// Returns 429 Too Many Requests when the limit is exceeded.
func (rl *RateLimiter) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := extractIP(r)
		if !rl.Allow(ip) {
			metrics.Global.RateLimitRejects.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "rate limit exceeded — try again later",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// cleanupLoop removes stale visitor entries every cleanup interval.
func (rl *RateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.cleanup)
	defer ticker.Stop()
	for range ticker.C {
		rl.mu.Lock()
		cutoff := time.Now().Add(-rl.cleanup)
		for ip, b := range rl.visitors {
			if b.lastSeen.Before(cutoff) {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// TrustProxy controls whether the rate limiter trusts proxy headers
// (X-Forwarded-For, X-Real-IP). When false, only r.RemoteAddr is used.
// Default: false (safe for direct exposure; set true when behind a trusted
// reverse proxy like nginx, Envoy, or a cloud load balancer).
var TrustProxy = false

// extractIP extracts the client IP from the request.
// When TrustProxy is true, checks X-Forwarded-For and X-Real-IP headers.
// Otherwise, uses r.RemoteAddr only (prevents IP spoofing via headers).
func extractIP(r *http.Request) string {
	if TrustProxy {
		// Check X-Forwarded-For (first IP in the chain is the client)
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			for i := 0; i < len(xff); i++ {
				if xff[i] == ',' {
					return strings.TrimSpace(xff[:i])
				}
			}
			return strings.TrimSpace(xff)
		}

		// Check X-Real-IP
		if xri := r.Header.Get("X-Real-IP"); xri != "" {
			return xri
		}
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

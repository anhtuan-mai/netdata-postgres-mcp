// SPDX-License-Identifier: GPL-3.0-or-later

// Package middleware provides HTTP middleware for the MCP server.
package middleware

import (
	"encoding/json"
	"net"
	"net/http"
	"sync"
	"time"
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

// extractIP extracts the client IP from the request, checking
// X-Forwarded-For and X-Real-IP headers before falling back to RemoteAddr.
func extractIP(r *http.Request) string {
	// Check X-Forwarded-For (first IP in the chain is the client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP
		for i := 0; i < len(xff); i++ {
			if xff[i] == ',' {
				return xff[:i]
			}
		}
		return xff
	}

	// Check X-Real-IP
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

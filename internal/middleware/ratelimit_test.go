// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimiter_Allow(t *testing.T) {
	rl := NewRateLimiter(2, 2) // 2 rps, burst of 2

	// First 2 requests should pass
	if !rl.Allow("1.2.3.4") {
		t.Error("request 1 should be allowed")
	}
	if !rl.Allow("1.2.3.4") {
		t.Error("request 2 should be allowed")
	}

	// Third request should be blocked (burst exhausted, no time for refill)
	if rl.Allow("1.2.3.4") {
		t.Error("request 3 should be blocked")
	}
}

func TestRateLimiter_DifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, 1) // 1 rps, burst of 1

	// Each IP gets its own bucket
	if !rl.Allow("1.1.1.1") {
		t.Error("IP 1 first request should be allowed")
	}
	if !rl.Allow("2.2.2.2") {
		t.Error("IP 2 first request should be allowed")
	}

	// Both should be blocked on second immediate request
	if rl.Allow("1.1.1.1") {
		t.Error("IP 1 second request should be blocked")
	}
	if rl.Allow("2.2.2.2") {
		t.Error("IP 2 second request should be blocked")
	}
}

func TestRateLimiter_Handler_OK(t *testing.T) {
	rl := NewRateLimiter(10, 10)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	handler := rl.Handler(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "5.5.5.5:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestRateLimiter_Handler_429(t *testing.T) {
	rl := NewRateLimiter(1, 1) // very tight limit

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := rl.Handler(inner)

	// First request OK
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "6.6.6.6:12345"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request: status = %d, want 200", rec.Code)
	}

	// Second request should be rate limited
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	req2.RemoteAddr = "6.6.6.6:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: status = %d, want 429", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") != "1" {
		t.Errorf("missing Retry-After header")
	}
}

func TestExtractIP_RemoteAddr(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:54321"
	if ip := extractIP(req); ip != "10.0.0.1" {
		t.Errorf("extractIP = %q, want 10.0.0.1", ip)
	}
}

func TestExtractIP_XForwardedFor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1")
	if ip := extractIP(req); ip != "1.2.3.4" {
		t.Errorf("extractIP = %q, want 1.2.3.4", ip)
	}
}

func TestExtractIP_XRealIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Real-IP", "9.8.7.6")
	if ip := extractIP(req); ip != "9.8.7.6" {
		t.Errorf("extractIP = %q, want 9.8.7.6", ip)
	}
}

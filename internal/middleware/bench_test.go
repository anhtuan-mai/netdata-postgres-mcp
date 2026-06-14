// SPDX-License-Identifier: GPL-3.0-or-later

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func BenchmarkRateLimiter_Allow(b *testing.B) {
	rl := NewRateLimiter(1000, 1000) // high limit to avoid rejections
	b.ResetTimer()
	for b.Loop() {
		rl.Allow("10.0.0.1")
	}
}

func BenchmarkRateLimiter_Allow_MultiIP(b *testing.B) {
	rl := NewRateLimiter(1000, 1000)
	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"}
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		rl.Allow(ips[i%len(ips)])
	}
}

func BenchmarkRateLimiter_Handler(b *testing.B) {
	rl := NewRateLimiter(100000, 100000)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := rl.Handler(inner)
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"

	b.ResetTimer()
	for b.Loop() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}
}

func BenchmarkExtractIP_RemoteAddr(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.168.1.100:54321"
	b.ResetTimer()
	for b.Loop() {
		extractIP(req)
	}
}

func BenchmarkExtractIP_XForwardedFor(b *testing.B) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.1, 172.16.0.1")
	b.ResetTimer()
	for b.Loop() {
		extractIP(req)
	}
}

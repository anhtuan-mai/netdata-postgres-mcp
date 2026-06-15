// SPDX-License-Identifier: GPL-3.0-or-later

package reload

import (
	"log/slog"
	"os"
	"testing"
)

func TestReload_LogLevel(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rc := New(logger)

	if rc.LogLevel() != "info" {
		t.Fatalf("expected info, got %s", rc.LogLevel())
	}

	t.Setenv("LOG_LEVEL", "debug")
	rc.Reload()

	if rc.LogLevel() != "debug" {
		t.Fatalf("expected debug after reload, got %s", rc.LogLevel())
	}
}

func TestReload_AuthToken(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rc := New(logger)

	if rc.AuthToken() != "" {
		t.Fatal("expected empty initial token")
	}

	t.Setenv("MCP_AUTH_TOKEN", "new-secret")
	rc.Reload()

	if rc.AuthToken() != "new-secret" {
		t.Fatalf("expected new-secret, got %s", rc.AuthToken())
	}
}

func TestReload_RateLimiter(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	rc := New(logger)

	if rc.RateLimiter() != nil {
		t.Fatal("expected nil rate limiter initially")
	}

	t.Setenv("RATE_LIMIT_RPS", "50")
	rc.Reload()

	if rc.RateLimiter() == nil {
		t.Fatal("expected non-nil rate limiter after reload")
	}
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"unknown", slog.LevelInfo},
	}
	for _, tt := range tests {
		if got := ParseLogLevel(tt.input); got != tt.want {
			t.Errorf("ParseLogLevel(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

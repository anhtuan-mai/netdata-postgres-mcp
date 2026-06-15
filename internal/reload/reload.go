// SPDX-License-Identifier: GPL-3.0-or-later

package reload

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/middleware"
)

type ReloadableConfig struct {
	mu          sync.RWMutex
	logLevel    atomic.Value
	rateLimiter atomic.Pointer[middleware.RateLimiter]
	authToken   atomic.Value
	logger      *slog.Logger
}

func New(logger *slog.Logger) *ReloadableConfig {
	rc := &ReloadableConfig{logger: logger}
	rc.logLevel.Store("info")
	rc.authToken.Store("")
	return rc
}

func (rc *ReloadableConfig) LogLevel() string {
	return rc.logLevel.Load().(string)
}

func (rc *ReloadableConfig) AuthToken() string {
	return rc.authToken.Load().(string)
}

func (rc *ReloadableConfig) RateLimiter() *middleware.RateLimiter {
	return rc.rateLimiter.Load()
}

func (rc *ReloadableConfig) Reload() {
	rc.mu.Lock()
	defer rc.mu.Unlock()

	if v := os.Getenv("LOG_LEVEL"); v != "" {
		old := rc.logLevel.Load().(string)
		if old != v {
			rc.logLevel.Store(v)
			rc.logger.Info("reloaded log level", "old", old, "new", v)
		}
	}

	if v := os.Getenv("MCP_AUTH_TOKEN"); v != "" {
		rc.authToken.Store(v)
		rc.logger.Info("reloaded auth token")
	}

	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if rps, err := strconv.ParseFloat(v, 64); err == nil && rps > 0 {
			rl := middleware.NewRateLimiter(rps, int(rps)*2+1)
			rc.rateLimiter.Store(rl)
			rc.logger.Info("reloaded rate limiter", "rps", rps)
		}
	}

	rc.logger.Info("config reload complete")
}

// ParseLogLevel converts a level string to slog.Level.
func ParseLogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

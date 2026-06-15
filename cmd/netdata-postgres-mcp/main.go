// SPDX-License-Identifier: GPL-3.0-or-later

// netdata-postgres-mcp is a sidecar service that collects hardware/system
// metrics from a Netdata Agent/Parent, stores snapshots in PostgreSQL, and
// exposes the stored data through an MCP (Model Context Protocol) server.
//
// Usage:
//
//	netdata-postgres-mcp migrate         Run database migrations
//	netdata-postgres-mcp collect-once    Collect metrics once and exit
//	netdata-postgres-mcp run             Start scheduler + MCP server
//	netdata-postgres-mcp mcp             Start MCP server only (stdio)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/collector"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/config"
	mcpserver "github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/mcp"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/metrics"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/middleware"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/remotewrite"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/scheduler"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/tracing"

	mcpstdio "github.com/mark3labs/mcp-go/server"
)

// version is set at build time via -ldflags "-X main.version=..."
// Falls back to "dev" for local builds.
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "migrate":
		runMigrate()
	case "collect-once":
		runCollectOnce()
	case "run":
		runService()
	case "mcp":
		runMCPOnly()
	case "version", "--version", "-v":
		fmt.Printf("netdata-postgres-mcp %s\n", version)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `netdata-postgres-mcp %s

A sidecar that stores Netdata metrics in PostgreSQL and serves them via MCP.

Commands:
  migrate        Run database migrations (idempotent, safe to re-run)
  collect-once   Collect metrics once and exit (useful for cron/testing)
  run            Start scheduler + MCP server (HTTP/SSE) — main production mode
  mcp            Start MCP server only (stdio transport for AI assistants)
  version        Show version
  help           Show this help

Subcommand Details:
  migrate:
    Applies all pending SQL migrations from internal/store/migrations/.
    Idempotent — safe to run multiple times. Creates tables for metrics,
    nodes, rollups, tenants, and API tokens.
    Example: netdata-postgres-mcp migrate

  collect-once:
    Connects to Netdata, collects one round of metrics, inserts into
    PostgreSQL, then exits. Useful for testing or cron-based collection.
    Example: POSTGRES_DSN=... NETDATA_BASE_URL=... netdata-postgres-mcp collect-once

  run:
    Main production mode. Starts the metric collection scheduler and
    HTTP/SSE MCP server. Serves /healthz, /readyz, /metrics (Prometheus),
    /sse, /message, and /api/v1/write (remote-write receiver).
    Supports SIGHUP for config hot-reload without restart.
    Example: netdata-postgres-mcp run

  mcp:
    Starts MCP server on stdio (stdin/stdout). Designed for direct
    connection from AI assistants like Claude Desktop or Cursor.
    Example: netdata-postgres-mcp mcp

Environment:
  CONFIG_FILE                  Path to YAML config (optional)
  NETDATA_BASE_URL             Netdata agent URL (default: http://localhost:19999)
  NETDATA_API_KEY              Netdata API key (optional)
  POSTGRES_DSN                 PostgreSQL connection string (required)
  COLLECTION_INTERVAL_SECONDS  Collection interval in seconds (default: 60)
  NODE_ID                      Node identifier (auto-detected if empty)
  ENABLED_CONTEXTS             Comma-separated metric contexts
  MCP_BIND_ADDR                MCP HTTP server address (default: 127.0.0.1:8765)
  LOG_LEVEL                    Log level: debug, info, warn, error (default: info)
  LOG_FORMAT                   Log format: text, json (default: text)
  RETENTION_DAYS               Delete samples older than N days (default: 30, 0 to disable)
  MCP_AUTH_TOKEN               Bearer token for MCP endpoints (optional, no auth if empty)
  RATE_LIMIT_RPS               Max requests per second per IP (default: 0 = disabled)
  TLS_CERT_FILE                Path to TLS certificate file (enables HTTPS)
  TLS_KEY_FILE                 Path to TLS private key file (enables HTTPS)
  NETDATA_NODES                Comma-separated Netdata URLs for multi-node collection
  OTEL_EXPORTER_OTLP_ENDPOINT  OpenTelemetry OTLP endpoint (optional, enables tracing)

Signals:
  SIGHUP     Reload configuration without restart
  SIGINT     Graceful shutdown
  SIGTERM    Graceful shutdown
`, version)
}

func loadConfig() config.Config {
	configFile := os.Getenv("CONFIG_FILE")
	cfg, err := config.Load(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

func newLogger(level, format string) *slog.Logger {
	var logLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn", "warning":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: logLevel}
	var handler slog.Handler
	if strings.ToLower(format) == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	// Wrap with trace ID correlation if tracing may be active
	handler = tracing.NewLogHandler(handler)

	return slog.New(handler)
}

func runMigrate() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel, cfg.LogFormat)

	ctx := context.Background()
	st, err := store.New(ctx, cfg.PostgresDSN, logger)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		logger.Error("migration failed", "error", err)
		os.Exit(1)
	}

	logger.Info("migrations completed successfully")
}

func runCollectOnce() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel, cfg.LogFormat)

	ctx := context.Background()
	st, err := store.New(ctx, cfg.PostgresDSN, logger)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	col := collector.New(cfg.NetdataBaseURL, cfg.NetdataAPIKey, cfg.NodeID,
		cfg.EnabledContexts, cfg.CollectionIntervalSeconds, logger)

	nodeID, err := col.ResolveNodeID(ctx)
	if err != nil {
		logger.Error("failed to resolve node ID", "error", err)
		os.Exit(1)
	}

	hostname, _ := os.Hostname()
	sched := scheduler.New(col, st, cfg.CollectionIntervalSeconds,
		nodeID, hostname, cfg.NetdataBaseURL, logger)

	if err := sched.CollectOnce(ctx); err != nil {
		logger.Error("collection failed", "error", err)
		os.Exit(1)
	}

	logger.Info("collection completed successfully")
}

func runService() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Initialize optional OpenTelemetry tracing
	shutdownTracing, err := tracing.Init(ctx, version, logger)
	if err != nil {
		logger.Warn("failed to initialize tracing", "error", err)
	} else {
		defer shutdownTracing(context.Background())
	}

	st, err := store.NewWithPoolOptions(ctx, cfg.PostgresDSN, logger, &store.PoolOptions{
		MinConns:          cfg.Pool.MinConns,
		MaxConns:          cfg.Pool.MaxConns,
		MaxConnLifetime:   cfg.Pool.MaxConnLifetime,
		MaxConnIdleTime:   cfg.Pool.MaxConnIdleTime,
		HealthCheckPeriod: cfg.Pool.HealthCheckPeriod,
	})
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	// Run migrations automatically
	if err := st.Migrate(ctx); err != nil {
		logger.Error("auto-migration failed", "error", err)
		os.Exit(1)
	}

	// Set up collectors and schedulers for all configured nodes
	nodes := cfg.EffectiveNodes()
	hostname, _ := os.Hostname()
	var schedulers []*scheduler.Scheduler

	for _, nodeCfg := range nodes {
		nodeLogger := logger.With("base_url", nodeCfg.BaseURL)
		col := collector.New(nodeCfg.BaseURL, nodeCfg.APIKey, nodeCfg.NodeID,
			cfg.EnabledContexts, cfg.CollectionIntervalSeconds, nodeLogger)

		nodeID, err := col.ResolveNodeID(ctx)
		if err != nil {
			nodeLogger.Error("failed to resolve node ID", "error", err)
			os.Exit(1)
		}

		sched := scheduler.New(col, st, cfg.CollectionIntervalSeconds,
			nodeID, hostname, nodeCfg.BaseURL, nodeLogger)
		sched.SetRetentionDays(cfg.RetentionDays)
		schedulers = append(schedulers, sched)
	}

	// Build HTTP mux with health endpoints + MCP SSE server
	mcpSrv := mcpserver.New(st.Pool(), logger)
	sseServer := mcpstdio.NewSSEServer(mcpSrv.MCPServer())

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/readyz", readyzHandler(st.Pool()))
	mux.HandleFunc("/metrics", metrics.Handler())

	// Remote-write receiver for Prometheus metrics
	rwNodeID := cfg.NodeID
	if rwNodeID == "" {
		rwNodeID = "remote-write"
	}
	rwHandler := remotewrite.NewHandler(st.Pool(), rwNodeID, logger)
	mux.Handle("/api/v1/write", rwHandler)
	logger.Info("Prometheus remote-write endpoint enabled", "path", "/api/v1/write")

	// Wrap MCP endpoints with auth middleware if configured
	var mcpHandler http.Handler = sseServer
	if cfg.MCPAuthToken != "" {
		mcpHandler = authMiddleware(cfg.MCPAuthToken, sseServer)
		logger.Info("MCP auth enabled — bearer token required for /sse and /message")
	}
	if cfg.RateLimitRPS > 0 {
		rl := middleware.NewRateLimiter(cfg.RateLimitRPS, int(cfg.RateLimitRPS)*2+1)
		mcpHandler = rl.Handler(mcpHandler)
		logger.Info("rate limiting enabled", "rps", cfg.RateLimitRPS)
	}
	mux.Handle("/", mcpHandler)

	httpSrv := &http.Server{
		Addr:              cfg.MCPBindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
			logger.Info("HTTPS server starting", "addr", cfg.MCPBindAddr,
				"endpoints", []string{"/healthz", "/readyz", "/metrics", "/sse", "/message"},
				"tls", true)
			if err := httpSrv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile); err != nil && err != http.ErrServerClosed {
				logger.Error("HTTPS server error", "error", err)
			}
		} else {
			logger.Info("HTTP server starting", "addr", cfg.MCPBindAddr,
				"endpoints", []string{"/healthz", "/readyz", "/metrics", "/sse", "/message"})
			if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("HTTP server error", "error", err)
			}
		}
	}()

	// SIGHUP config reload
	go func() {
		sighup := make(chan os.Signal, 1)
		signal.Notify(sighup, syscall.SIGHUP)
		for range sighup {
			logger.Info("received SIGHUP, reloading configuration")
			newCfg, err := config.Load(os.Getenv("CONFIG_FILE"))
			if err != nil {
				logger.Error("config reload failed", "error", err)
				continue
			}
			// Apply hot-reloadable settings
			if newCfg.RetentionDays != cfg.RetentionDays {
				for _, s := range schedulers {
					s.SetRetentionDays(newCfg.RetentionDays)
				}
				logger.Info("reloaded retention_days", "value", newCfg.RetentionDays)
			}
			cfg = newCfg
			logger.Info("configuration reloaded successfully")
		}
	}()

	// Start collection schedulers (first one blocks, rest run as goroutines)
	logger.Info("starting schedulers and MCP server",
		"node_count", len(schedulers),
		"mcp_addr", cfg.MCPBindAddr,
		"interval", cfg.CollectionIntervalSeconds,
	)

	// Run additional schedulers as goroutines
	for i := 1; i < len(schedulers); i++ {
		go func(s *scheduler.Scheduler) {
			if err := s.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("scheduler error", "error", err)
			}
		}(schedulers[i])
	}

	// First scheduler blocks the main goroutine
	if err := schedulers[0].Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("scheduler error", "error", err)
		os.Exit(1)
	}

	// Graceful shutdown of HTTP/SSE server
	logger.Info("shutting down HTTP server")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", "error", err)
	}
	if err := sseServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("SSE server shutdown error", "error", err)
	}

	logger.Info("shutdown complete")
}

func runMCPOnly() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel, cfg.LogFormat)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := store.New(ctx, cfg.PostgresDSN, logger)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer st.Close()

	mcpSrv := mcpserver.New(st.Pool(), logger)

	// Use stdio transport for direct AI assistant connection
	logger.Info("starting MCP server on stdio")
	stdio := mcpstdio.NewStdioServer(mcpSrv.MCPServer())
	if err := stdio.Listen(ctx, os.Stdin, os.Stdout); err != nil && ctx.Err() == nil {
		logger.Error("MCP stdio error", "error", err)
		os.Exit(1)
	}
	logger.Info("MCP stdio server stopped")
}

// healthzHandler returns 200 if the process is alive.
func healthzHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": version,
	})
}

// readyzHandler returns 200 if the database is reachable, 503 otherwise.
func readyzHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		w.Header().Set("Content-Type", "application/json")

		if err := pool.Ping(ctx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{
				"status": "error",
				"reason": "database unreachable",
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ready",
		})
	}
}

// authMiddleware validates bearer token for MCP endpoints.
// Health endpoints (/healthz, /readyz) are NOT wrapped by this.
func authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			// Also check query parameter for SSE clients that can't set headers
			if q := r.URL.Query().Get("token"); q != "" {
				auth = "Bearer " + q
			}
		}

		expected := "Bearer " + token
		if auth != expected {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "unauthorized — provide Authorization: Bearer <token>",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

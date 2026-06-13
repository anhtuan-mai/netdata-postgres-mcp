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
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/collector"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/config"
	mcpserver "github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/mcp"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/scheduler"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"

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
  migrate        Run database migrations
  collect-once   Collect metrics once and exit
  run            Start scheduler + MCP server (HTTP/SSE)
  mcp            Start MCP server only (stdio transport)
  version        Show version
  help           Show this help

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

func newLogger(level string) *slog.Logger {
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

	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
}

func runMigrate() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel)

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
	logger := newLogger(cfg.LogLevel)

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
	logger := newLogger(cfg.LogLevel)

	ctx, cancel := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	st, err := store.New(ctx, cfg.PostgresDSN, logger)
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

	// Start MCP HTTP/SSE server in background
	mcpSrv := mcpserver.New(st.Pool(), logger)
	sseServer := mcpstdio.NewSSEServer(mcpSrv.MCPServer())

	go func() {
		logger.Info("MCP SSE server starting", "addr", cfg.MCPBindAddr)
		if err := sseServer.Start(cfg.MCPBindAddr); err != nil && err != http.ErrServerClosed {
			logger.Error("MCP server error", "error", err)
		}
	}()

	// Start collection scheduler (blocks until ctx cancelled)
	logger.Info("starting scheduler and MCP server",
		"node_id", nodeID,
		"mcp_addr", cfg.MCPBindAddr,
		"interval", cfg.CollectionIntervalSeconds,
	)

	if err := sched.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("scheduler error", "error", err)
		os.Exit(1)
	}

	logger.Info("shutting down")
}

func runMCPOnly() {
	cfg := loadConfig()
	logger := newLogger(cfg.LogLevel)

	ctx := context.Background()
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
	if err := stdio.Listen(ctx, os.Stdin, os.Stdout); err != nil {
		logger.Error("MCP stdio error", "error", err)
		os.Exit(1)
	}
}

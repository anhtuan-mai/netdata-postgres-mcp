// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()

	if cfg.NetdataBaseURL != "http://localhost:19999" {
		t.Errorf("expected default NetdataBaseURL http://localhost:19999, got %s", cfg.NetdataBaseURL)
	}
	if cfg.CollectionIntervalSeconds != 60 {
		t.Errorf("expected default interval 60, got %d", cfg.CollectionIntervalSeconds)
	}
	if cfg.MCPBindAddr != "127.0.0.1:8765" {
		t.Errorf("expected default MCPBindAddr 127.0.0.1:8765, got %s", cfg.MCPBindAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("expected default LogLevel info, got %s", cfg.LogLevel)
	}
	if len(cfg.EnabledContexts) != len(DefaultContexts) {
		t.Errorf("expected %d default contexts, got %d", len(DefaultContexts), len(cfg.EnabledContexts))
	}
}

func TestLoadFromYAML(t *testing.T) {
	yamlContent := `
netdata_base_url: http://remote:19999
postgres_dsn: postgres://user:pass@db:5432/metrics
collection_interval_seconds: 30
node_id: test-node
enabled_contexts:
  - system.cpu
  - system.ram
mcp_bind_addr: 0.0.0.0:9999
log_level: debug
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.NetdataBaseURL != "http://remote:19999" {
		t.Errorf("expected http://remote:19999, got %s", cfg.NetdataBaseURL)
	}
	if cfg.PostgresDSN != "postgres://user:pass@db:5432/metrics" {
		t.Errorf("expected postgres DSN from file, got %s", cfg.PostgresDSN)
	}
	if cfg.CollectionIntervalSeconds != 30 {
		t.Errorf("expected 30, got %d", cfg.CollectionIntervalSeconds)
	}
	if cfg.NodeID != "test-node" {
		t.Errorf("expected test-node, got %s", cfg.NodeID)
	}
	if len(cfg.EnabledContexts) != 2 {
		t.Errorf("expected 2 contexts, got %d", len(cfg.EnabledContexts))
	}
	if cfg.MCPBindAddr != "0.0.0.0:9999" {
		t.Errorf("expected 0.0.0.0:9999, got %s", cfg.MCPBindAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("expected debug, got %s", cfg.LogLevel)
	}
}

func TestEnvOverridesFile(t *testing.T) {
	yamlContent := `
netdata_base_url: http://file:19999
postgres_dsn: postgres://file@db/metrics
collection_interval_seconds: 30
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("NETDATA_BASE_URL", "http://env:19999")
	t.Setenv("POSTGRES_DSN", "postgres://env@db/metrics")
	t.Setenv("COLLECTION_INTERVAL_SECONDS", "120")
	t.Setenv("NODE_ID", "env-node")
	t.Setenv("ENABLED_CONTEXTS", "system.cpu, system.ram, system.io")
	t.Setenv("MCP_BIND_ADDR", "0.0.0.0:7777")
	t.Setenv("LOG_LEVEL", "warn")

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}

	if cfg.NetdataBaseURL != "http://env:19999" {
		t.Errorf("env override failed for NetdataBaseURL: got %s", cfg.NetdataBaseURL)
	}
	if cfg.PostgresDSN != "postgres://env@db/metrics" {
		t.Errorf("env override failed for PostgresDSN: got %s", cfg.PostgresDSN)
	}
	if cfg.CollectionIntervalSeconds != 120 {
		t.Errorf("env override failed for interval: got %d", cfg.CollectionIntervalSeconds)
	}
	if cfg.NodeID != "env-node" {
		t.Errorf("env override failed for NodeID: got %s", cfg.NodeID)
	}
	if len(cfg.EnabledContexts) != 3 {
		t.Errorf("env override failed for contexts: got %d", len(cfg.EnabledContexts))
	}
	if cfg.MCPBindAddr != "0.0.0.0:7777" {
		t.Errorf("env override failed for MCPBindAddr: got %s", cfg.MCPBindAddr)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("env override failed for LogLevel: got %s", cfg.LogLevel)
	}
}

func TestValidateMissingDSN(t *testing.T) {
	cfg := Defaults()
	// No DSN set
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for missing postgres_dsn")
	}
}

func TestValidateBadInterval(t *testing.T) {
	cfg := Defaults()
	cfg.PostgresDSN = "postgres://test@localhost/db"
	cfg.CollectionIntervalSeconds = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for zero interval")
	}
}

func TestLoadNoFile(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://test@localhost/db")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://test@localhost/db" {
		t.Errorf("expected DSN from env, got %s", cfg.PostgresDSN)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing config file")
	}
}

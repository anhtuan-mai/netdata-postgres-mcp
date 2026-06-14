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
		t.Errorf("NetdataBaseURL = %q, want %q", cfg.NetdataBaseURL, "http://localhost:19999")
	}
	if cfg.CollectionIntervalSeconds != 60 {
		t.Errorf("CollectionIntervalSeconds = %d, want 60", cfg.CollectionIntervalSeconds)
	}
	if cfg.MCPBindAddr != "127.0.0.1:8765" {
		t.Errorf("MCPBindAddr = %q, want %q", cfg.MCPBindAddr, "127.0.0.1:8765")
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
	if cfg.RetentionDays != 30 {
		t.Errorf("RetentionDays = %d, want 30", cfg.RetentionDays)
	}
	if len(cfg.EnabledContexts) == 0 {
		t.Error("EnabledContexts should not be empty")
	}
}

func TestValidate_MissingDSN(t *testing.T) {
	cfg := Defaults()
	cfg.PostgresDSN = ""
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for missing PostgresDSN")
	}
}

func TestValidate_BadInterval(t *testing.T) {
	cfg := Defaults()
	cfg.PostgresDSN = "postgres://localhost/test"
	cfg.CollectionIntervalSeconds = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for zero interval")
	}
}

func TestValidate_EmptyContexts(t *testing.T) {
	cfg := Defaults()
	cfg.PostgresDSN = "postgres://localhost/test"
	cfg.EnabledContexts = nil
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for empty contexts")
	}
}

func TestValidate_OK(t *testing.T) {
	cfg := Defaults()
	cfg.PostgresDSN = "postgres://localhost/test"
	if err := cfg.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadFromEnv(t *testing.T) {
	envs := map[string]string{
		"NETDATA_BASE_URL":            "http://10.0.0.1:19999",
		"POSTGRES_DSN":                "postgres://u:p@db:5432/metrics",
		"COLLECTION_INTERVAL_SECONDS": "30",
		"NODE_ID":                     "test-node-1",
		"ENABLED_CONTEXTS":            "system.cpu,system.ram",
		"MCP_BIND_ADDR":               "0.0.0.0:9000",
		"LOG_LEVEL":                   "debug",
		"LOG_FORMAT":                   "json",
		"RETENTION_DAYS":              "7",
		"MCP_AUTH_TOKEN":              "secret123",
	}
	for k, v := range envs {
		t.Setenv(k, v)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NetdataBaseURL != "http://10.0.0.1:19999" {
		t.Errorf("NetdataBaseURL = %q, want %q", cfg.NetdataBaseURL, "http://10.0.0.1:19999")
	}
	if cfg.PostgresDSN != "postgres://u:p@db:5432/metrics" {
		t.Errorf("PostgresDSN = %q", cfg.PostgresDSN)
	}
	if cfg.CollectionIntervalSeconds != 30 {
		t.Errorf("CollectionIntervalSeconds = %d, want 30", cfg.CollectionIntervalSeconds)
	}
	if cfg.NodeID != "test-node-1" {
		t.Errorf("NodeID = %q, want %q", cfg.NodeID, "test-node-1")
	}
	if len(cfg.EnabledContexts) != 2 || cfg.EnabledContexts[0] != "system.cpu" {
		t.Errorf("EnabledContexts = %v", cfg.EnabledContexts)
	}
	if cfg.MCPBindAddr != "0.0.0.0:9000" {
		t.Errorf("MCPBindAddr = %q", cfg.MCPBindAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q", cfg.LogLevel)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q", cfg.LogFormat)
	}
	if cfg.RetentionDays != 7 {
		t.Errorf("RetentionDays = %d, want 7", cfg.RetentionDays)
	}
	if cfg.MCPAuthToken != "secret123" {
		t.Errorf("MCPAuthToken = %q", cfg.MCPAuthToken)
	}
}

func TestLoadFromYAML(t *testing.T) {
	yaml := `
netdata_base_url: http://yaml-host:19999
postgres_dsn: postgres://yaml@localhost/db
collection_interval_seconds: 120
log_level: warn
log_format: json
retention_days: 14
enabled_contexts:
  - system.cpu
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.NetdataBaseURL != "http://yaml-host:19999" {
		t.Errorf("NetdataBaseURL = %q", cfg.NetdataBaseURL)
	}
	if cfg.CollectionIntervalSeconds != 120 {
		t.Errorf("CollectionIntervalSeconds = %d, want 120", cfg.CollectionIntervalSeconds)
	}
	if cfg.RetentionDays != 14 {
		t.Errorf("RetentionDays = %d, want 14", cfg.RetentionDays)
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json", cfg.LogFormat)
	}
}

func TestEnvOverridesYAML(t *testing.T) {
	yaml := `
postgres_dsn: postgres://yaml@localhost/db
log_level: warn
`
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("POSTGRES_DSN", "postgres://env@localhost/db")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want %q (env should override YAML)", cfg.LogLevel, "error")
	}
	if cfg.PostgresDSN != "postgres://env@localhost/db" {
		t.Errorf("PostgresDSN = %q, want env override", cfg.PostgresDSN)
	}
}

func TestLoadBadFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

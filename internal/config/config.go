// SPDX-License-Identifier: GPL-3.0-or-later

// Package config provides configuration loading for netdata-postgres-mcp.
// Configuration values can be set via YAML config file and/or environment
// variables. Environment variables take precedence over file values.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultContexts is the default set of Netdata metric contexts to collect.
var DefaultContexts = []string{
	"system.cpu",
	"system.ram",
	"system.swap",
	"system.io",
	"system.pgpgio",
	"system.ip",
	"disk.io",
	"disk.ops",
	"disk.util",
	"disk.space",
	"disk.inodes",
	"apps.cpu",
	"apps.mem",
}

// Config holds all configuration for the sidecar service.
type Config struct {
	NetdataBaseURL            string   `yaml:"netdata_base_url"`
	NetdataAPIKey             string   `yaml:"netdata_api_key"`
	PostgresDSN               string   `yaml:"postgres_dsn"`
	CollectionIntervalSeconds int      `yaml:"collection_interval_seconds"`
	NodeID                    string   `yaml:"node_id"`
	EnabledContexts           []string `yaml:"enabled_contexts"`
	MCPBindAddr               string   `yaml:"mcp_bind_addr"`
	LogLevel                  string   `yaml:"log_level"`
	RetentionDays             int      `yaml:"retention_days"`
	MCPAuthToken              string   `yaml:"mcp_auth_token"`
}

// Defaults returns a Config with all default values applied.
func Defaults() Config {
	return Config{
		NetdataBaseURL:            "http://localhost:19999",
		CollectionIntervalSeconds: 60,
		EnabledContexts:           append([]string{}, DefaultContexts...),
		MCPBindAddr:               "127.0.0.1:8765",
		LogLevel:                  "info",
		RetentionDays:             30,
	}
}

// Load reads configuration from the given YAML file path (may be empty) and
// then applies environment variable overrides. Environment variables always
// take precedence over file values.
func Load(path string) (Config, error) {
	cfg := Defaults()

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("reading config file %s: %w", path, err)
		}
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parsing config file %s: %w", path, err)
		}
	}

	applyEnv(&cfg)

	if err := cfg.Validate(); err != nil {
		return cfg, err
	}

	return cfg, nil
}

// Validate checks that required fields are set and values are sane.
func (c *Config) Validate() error {
	if c.PostgresDSN == "" {
		return fmt.Errorf("postgres_dsn is required (set POSTGRES_DSN or postgres_dsn in config)")
	}
	if c.CollectionIntervalSeconds < 1 {
		return fmt.Errorf("collection_interval_seconds must be >= 1, got %d", c.CollectionIntervalSeconds)
	}
	if len(c.EnabledContexts) == 0 {
		return fmt.Errorf("enabled_contexts must not be empty")
	}
	return nil
}

// applyEnv overrides config fields from environment variables.
func applyEnv(cfg *Config) {
	if v := os.Getenv("NETDATA_BASE_URL"); v != "" {
		cfg.NetdataBaseURL = v
	}
	if v := os.Getenv("NETDATA_API_KEY"); v != "" {
		cfg.NetdataAPIKey = v
	}
	if v := os.Getenv("POSTGRES_DSN"); v != "" {
		cfg.PostgresDSN = v
	}
	if v := os.Getenv("COLLECTION_INTERVAL_SECONDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.CollectionIntervalSeconds = n
		}
	}
	if v := os.Getenv("NODE_ID"); v != "" {
		cfg.NodeID = v
	}
	if v := os.Getenv("ENABLED_CONTEXTS"); v != "" {
		parts := strings.Split(v, ",")
		contexts := make([]string, 0, len(parts))
		for _, p := range parts {
			if t := strings.TrimSpace(p); t != "" {
				contexts = append(contexts, t)
			}
		}
		if len(contexts) > 0 {
			cfg.EnabledContexts = contexts
		}
	}
	if v := os.Getenv("MCP_BIND_ADDR"); v != "" {
		cfg.MCPBindAddr = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}
	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RetentionDays = n
		}
	}
	if v := os.Getenv("MCP_AUTH_TOKEN"); v != "" {
		cfg.MCPAuthToken = v
	}
}

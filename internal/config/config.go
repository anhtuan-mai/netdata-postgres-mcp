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
	LogFormat                 string   `yaml:"log_format"`
	RetentionDays             int      `yaml:"retention_days"`
	MCPAuthToken              string   `yaml:"mcp_auth_token"`
	RateLimitRPS              float64  `yaml:"rate_limit_rps"`
	TLSCertFile               string           `yaml:"tls_cert_file"`
	TLSKeyFile                string           `yaml:"tls_key_file"`
	Nodes                     []NodeConfig     `yaml:"nodes"`
	DerivedContexts           []DerivedContext  `yaml:"derived_contexts"`
	Pool                      PoolConfig       `yaml:"pool"`
}

// NodeConfig defines a single Netdata node to collect from.
// Used in multi-node mode when multiple agents are monitored by one sidecar.
type NodeConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	NodeID  string `yaml:"node_id"`
}

// Defaults returns a Config with all default values applied.
func Defaults() Config {
	return Config{
		NetdataBaseURL:            "http://localhost:19999",
		CollectionIntervalSeconds: 60,
		EnabledContexts:           append([]string{}, DefaultContexts...),
		MCPBindAddr:               "127.0.0.1:8765",
		LogLevel:                  "info",
		LogFormat:                 "text",
		RetentionDays:             30,
		Pool:                      DefaultPoolConfig(),
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

// EffectiveNodes returns the list of nodes to collect from.
// If Nodes is empty, it returns a single node from NetdataBaseURL/NetdataAPIKey/NodeID
// for backward compatibility with single-node configuration.
func (c *Config) EffectiveNodes() []NodeConfig {
	if len(c.Nodes) > 0 {
		return c.Nodes
	}
	return []NodeConfig{{
		BaseURL: c.NetdataBaseURL,
		APIKey:  c.NetdataAPIKey,
		NodeID:  c.NodeID,
	}}
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
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
	}
	if v := os.Getenv("RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.RetentionDays = n
		}
	}
	if v := os.Getenv("MCP_AUTH_TOKEN"); v != "" {
		cfg.MCPAuthToken = v
	}
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			cfg.RateLimitRPS = n
		}
	}
	if v := os.Getenv("TLS_CERT_FILE"); v != "" {
		cfg.TLSCertFile = v
	}
	if v := os.Getenv("TLS_KEY_FILE"); v != "" {
		cfg.TLSKeyFile = v
	}
	if v := os.Getenv("NETDATA_NODES"); v != "" {
		parts := strings.Split(v, ",")
		nodes := make([]NodeConfig, 0, len(parts))
		for _, p := range parts {
			if u := strings.TrimSpace(p); u != "" {
				nodes = append(nodes, NodeConfig{BaseURL: u})
			}
		}
		if len(nodes) > 0 {
			cfg.Nodes = nodes
		}
	}
}

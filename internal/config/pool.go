// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"time"
)

// PoolConfig holds pgxpool tuning parameters.
type PoolConfig struct {
	MinConns          int32         `yaml:"min_conns"`
	MaxConns          int32         `yaml:"max_conns"`
	MaxConnLifetime   time.Duration `yaml:"max_conn_lifetime"`
	MaxConnIdleTime   time.Duration `yaml:"max_conn_idle_time"`
	HealthCheckPeriod time.Duration `yaml:"health_check_period"`
}

// DefaultPoolConfig returns sensible defaults for connection pooling.
func DefaultPoolConfig() PoolConfig {
	return PoolConfig{
		MinConns:          2,
		MaxConns:          10,
		MaxConnLifetime:   30 * time.Minute,
		MaxConnIdleTime:   5 * time.Minute,
		HealthCheckPeriod: 30 * time.Second,
	}
}

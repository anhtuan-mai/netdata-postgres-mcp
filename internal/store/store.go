// SPDX-License-Identifier: GPL-3.0-or-later

// Package store provides PostgreSQL storage for Netdata metrics.
// It handles connection management, schema migrations, and batched inserts
// with duplicate-safe upsert logic.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// MetricSample represents a single metric data point collected from Netdata.
type MetricSample struct {
	NodeID      string
	CollectedAt time.Time
	Context     string
	Chart       string
	Family      string
	Instance    string
	Dimension   string
	Unit        string
	Value       float64
	Labels      map[string]string
}

// NodeInfo represents a registered Netdata node.
type NodeInfo struct {
	NodeID          string    `json:"node_id"`
	Hostname        string    `json:"hostname"`
	NetdataBaseURL  string    `json:"netdata_base_url"`
	LastCollectedAt *time.Time `json:"last_collected_at,omitempty"`
}

// Store manages PostgreSQL connections and operations.
type Store struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// New creates a new Store with a connection pool to the given DSN.
func New(ctx context.Context, dsn string, logger *slog.Logger) (*Store, error) {
	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parsing postgres DSN: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connecting to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging postgres: %w", err)
	}

	return &Store{pool: pool, logger: logger}, nil
}

// Close shuts down the connection pool.
func (s *Store) Close() {
	s.pool.Close()
}

// Pool returns the underlying connection pool for direct queries (used by MCP tools).
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Migrate runs all SQL migration files in order.
func (s *Store) Migrate(ctx context.Context) error {
	// Create migrations tracking table
	_, err := s.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("creating migrations table: %w", err)
	}

	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("reading embedded migrations: %w", err)
	}

	// Sort by filename to ensure ordering
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := entry.Name()

		// Check if already applied
		var exists bool
		err := s.pool.QueryRow(ctx,
			"SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)",
			version,
		).Scan(&exists)
		if err != nil {
			return fmt.Errorf("checking migration %s: %w", version, err)
		}
		if exists {
			s.logger.Info("migration already applied", "version", version)
			continue
		}

		// Read and execute migration
		data, err := migrationsFS.ReadFile("migrations/" + version)
		if err != nil {
			return fmt.Errorf("reading migration %s: %w", version, err)
		}

		s.logger.Info("applying migration", "version", version)
		if _, err := s.pool.Exec(ctx, string(data)); err != nil {
			return fmt.Errorf("applying migration %s: %w", version, err)
		}

		// Record migration
		if _, err := s.pool.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)", version,
		); err != nil {
			return fmt.Errorf("recording migration %s: %w", version, err)
		}

		s.logger.Info("migration applied", "version", version)
	}

	return nil
}

// UpsertNode ensures a node record exists and updates its metadata.
func (s *Store) UpsertNode(ctx context.Context, nodeID, hostname, baseURL string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO netdata_nodes (node_id, hostname, netdata_base_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (node_id)
		DO UPDATE SET
			hostname = EXCLUDED.hostname,
			netdata_base_url = EXCLUDED.netdata_base_url,
			updated_at = now()
	`, nodeID, hostname, baseURL)
	if err != nil {
		return fmt.Errorf("upserting node %s: %w", nodeID, err)
	}
	return nil
}

// InsertSamples writes a batch of metric samples in a single transaction.
// Duplicates (same node/time/context/dimension/chart/instance) are skipped
// via ON CONFLICT DO NOTHING.
func (s *Store) InsertSamples(ctx context.Context, samples []MetricSample) (int64, error) {
	if len(samples) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Use COPY protocol for high-throughput inserts, falling back to
	// batched INSERT for conflict handling.
	const batchSize = 500
	var totalInserted int64

	for i := 0; i < len(samples); i += batchSize {
		end := i + batchSize
		if end > len(samples) {
			end = len(samples)
		}
		batch := samples[i:end]

		inserted, err := s.insertBatch(ctx, tx, batch)
		if err != nil {
			return totalInserted, fmt.Errorf("inserting batch at offset %d: %w", i, err)
		}
		totalInserted += inserted
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing transaction: %w", err)
	}

	return totalInserted, nil
}

// insertBatch inserts a sub-batch of samples using a single multi-row INSERT.
func (s *Store) insertBatch(ctx context.Context, tx pgx.Tx, samples []MetricSample) (int64, error) {
	if len(samples) == 0 {
		return 0, nil
	}

	// Build multi-row INSERT with ON CONFLICT DO NOTHING
	var b strings.Builder
	b.WriteString(`INSERT INTO hardware_metric_samples
		(node_id, collected_at, context, chart, family, instance, dimension, unit, value, labels)
		VALUES `)

	args := make([]interface{}, 0, len(samples)*10)
	for i, s := range samples {
		if i > 0 {
			b.WriteString(", ")
		}
		base := i * 10
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base+1, base+2, base+3, base+4, base+5,
			base+6, base+7, base+8, base+9, base+10)

		labels := s.Labels
		if labels == nil {
			labels = map[string]string{}
		}

		args = append(args,
			s.NodeID,
			s.CollectedAt,
			s.Context,
			s.Chart,
			s.Family,
			s.Instance,
			s.Dimension,
			s.Unit,
			s.Value,
			labels,
		)
	}

	b.WriteString(" ON CONFLICT ON CONSTRAINT uq_metric_sample DO NOTHING")

	tag, err := tx.Exec(ctx, b.String(), args...)
	if err != nil {
		return 0, err
	}

	return tag.RowsAffected(), nil
}

// ListNodes returns all registered nodes with their last collection time.
func (s *Store) ListNodes(ctx context.Context) ([]NodeInfo, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			n.node_id,
			COALESCE(n.hostname, ''),
			COALESCE(n.netdata_base_url, ''),
			(SELECT MAX(collected_at) FROM hardware_metric_samples WHERE node_id = n.node_id)
		FROM netdata_nodes n
		ORDER BY n.node_id
	`)
	if err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}
	defer rows.Close()

	var nodes []NodeInfo
	for rows.Next() {
		var n NodeInfo
		if err := rows.Scan(&n.NodeID, &n.Hostname, &n.NetdataBaseURL, &n.LastCollectedAt); err != nil {
			return nil, fmt.Errorf("scanning node: %w", err)
		}
		nodes = append(nodes, n)
	}

	return nodes, rows.Err()
}

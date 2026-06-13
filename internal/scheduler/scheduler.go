// SPDX-License-Identifier: GPL-3.0-or-later

// Package scheduler runs periodic metric collection from Netdata to PostgreSQL.
// It handles ticker-based scheduling, graceful shutdown, and error recovery
// to keep the collection loop running even when individual collections fail.
package scheduler

import (
	"context"
	"log/slog"
	"time"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/collector"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"
)

// Scheduler periodically collects metrics and stores them.
type Scheduler struct {
	collector *collector.Collector
	store     *store.Store
	interval  time.Duration
	nodeID    string
	hostname  string
	baseURL   string
	logger    *slog.Logger
}

// New creates a Scheduler that collects every intervalSec seconds.
func New(
	col *collector.Collector,
	st *store.Store,
	intervalSec int,
	nodeID, hostname, baseURL string,
	logger *slog.Logger,
) *Scheduler {
	return &Scheduler{
		collector: col,
		store:     st,
		interval:  time.Duration(intervalSec) * time.Second,
		nodeID:    nodeID,
		hostname:  hostname,
		baseURL:   baseURL,
		logger:    logger,
	}
}

// Run starts the collection loop. It blocks until ctx is cancelled.
// Errors during individual collection cycles are logged but do not
// stop the scheduler.
func (s *Scheduler) Run(ctx context.Context) error {
	s.logger.Info("scheduler starting",
		"interval", s.interval.String(),
		"node_id", s.nodeID,
	)

	// Run an initial collection immediately
	s.collectOnce(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("scheduler stopping")
			return ctx.Err()
		case <-ticker.C:
			s.collectOnce(ctx)
		}
	}
}

// CollectOnce performs a single collection cycle. Exported for the
// `collect-once` CLI command.
func (s *Scheduler) CollectOnce(ctx context.Context) error {
	return s.collectOnce(ctx)
}

func (s *Scheduler) collectOnce(ctx context.Context) error {
	start := time.Now()
	s.logger.Info("starting collection cycle")

	// Ensure node record exists
	if err := s.store.UpsertNode(ctx, s.nodeID, s.hostname, s.baseURL); err != nil {
		s.logger.Error("failed to upsert node", "error", err)
		return err
	}

	// Collect metrics from Netdata
	samples, err := s.collector.Collect(ctx)
	if err != nil {
		s.logger.Error("failed to collect metrics", "error", err)
		return err
	}

	if len(samples) == 0 {
		s.logger.Warn("no metrics collected")
		return nil
	}

	// Insert into PostgreSQL
	inserted, err := s.store.InsertSamples(ctx, samples)
	if err != nil {
		s.logger.Error("failed to insert samples", "error", err, "total", len(samples))
		return err
	}

	elapsed := time.Since(start)
	s.logger.Info("collection cycle complete",
		"samples_collected", len(samples),
		"samples_inserted", inserted,
		"duration", elapsed.String(),
	)

	return nil
}

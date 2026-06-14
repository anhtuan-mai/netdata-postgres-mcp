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
	collector     *collector.Collector
	store         *store.Store
	interval      time.Duration
	nodeID        string
	hostname      string
	baseURL       string
	retentionDays int
	logger        *slog.Logger
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
		collector:     col,
		store:         st,
		interval:      time.Duration(intervalSec) * time.Second,
		nodeID:        nodeID,
		hostname:      hostname,
		baseURL:       baseURL,
		retentionDays: 30, // default, overridden by SetRetentionDays
		logger:        logger,
	}
}

// SetRetentionDays configures automatic data retention. Set to 0 to disable.
func (s *Scheduler) SetRetentionDays(days int) {
	s.retentionDays = days
}

// Run starts the collection loop. It blocks until ctx is cancelled.
// Errors during individual collection cycles are logged but do not
// stop the scheduler.
func (s *Scheduler) Run(ctx context.Context) error {
	s.logger.Info("scheduler starting",
		"interval", s.interval.String(),
		"node_id", s.nodeID,
		"retention_days", s.retentionDays,
	)

	// Start retention cleanup goroutine
	if s.retentionDays > 0 {
		go s.retentionLoop(ctx)
	}

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

	// Collect metrics from Netdata with retry
	var samples []store.MetricSample
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		var err error
		samples, err = s.collector.Collect(ctx)
		if err == nil {
			break
		}
		lastErr = err
		if attempt < 3 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second // 1s, 2s
			s.logger.Warn("collection failed, retrying",
				"attempt", attempt, "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	if lastErr != nil && len(samples) == 0 {
		s.logger.Error("failed to collect metrics after 3 attempts", "error", lastErr)
		return lastErr
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

// retentionLoop runs daily to delete samples older than retentionDays.
func (s *Scheduler) retentionLoop(ctx context.Context) {
	retention := time.Duration(s.retentionDays) * 24 * time.Hour
	s.logger.Info("retention cleanup enabled", "retention", retention.String())

	// Run once at startup
	s.runRetention(ctx, retention)

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRetention(ctx, retention)
		}
	}
}

func (s *Scheduler) runRetention(ctx context.Context, retention time.Duration) {
	deleted, err := s.store.DeleteOldSamples(ctx, retention)
	if err != nil {
		s.logger.Error("retention cleanup failed", "error", err)
		return
	}
	if deleted > 0 {
		s.logger.Info("retention cleanup complete", "deleted", deleted)
	}
}

// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"context"
	"fmt"
	"time"
)

// AggregateHourly rolls up raw samples into hourly buckets.
// Processes samples from the given time range that haven't been aggregated yet.
// Returns the number of rollup rows upserted.
func (s *Store) AggregateHourly(ctx context.Context, before time.Time) (int64, error) {
	// Aggregate all samples older than 'before', grouped into hourly buckets.
	// Uses ON CONFLICT to be idempotent — safe to re-run on overlapping ranges.
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO hardware_metric_rollups_1h
			(node_id, bucket, context, dimension, instance, unit, avg_value, min_value, max_value, sample_count)
		SELECT
			node_id,
			date_trunc('hour', collected_at) AS bucket,
			context,
			dimension,
			COALESCE(instance, '') AS instance,
			COALESCE(MAX(unit), '') AS unit,
			AVG(value) AS avg_value,
			MIN(value) AS min_value,
			MAX(value) AS max_value,
			COUNT(*)::INTEGER AS sample_count
		FROM hardware_metric_samples
		WHERE collected_at < $1
		GROUP BY node_id, date_trunc('hour', collected_at), context, dimension, COALESCE(instance, '')
		ON CONFLICT ON CONSTRAINT uq_rollup_1h
		DO UPDATE SET
			avg_value = EXCLUDED.avg_value,
			min_value = EXCLUDED.min_value,
			max_value = EXCLUDED.max_value,
			sample_count = EXCLUDED.sample_count,
			created_at = now()
	`, before)
	if err != nil {
		return 0, fmt.Errorf("aggregating hourly rollups: %w", err)
	}
	return tag.RowsAffected(), nil
}

// AggregateDaily rolls up hourly rollups into daily buckets.
// Returns the number of rollup rows upserted.
func (s *Store) AggregateDaily(ctx context.Context, before time.Time) (int64, error) {
	tag, err := s.pool.Exec(ctx, `
		INSERT INTO hardware_metric_rollups_1d
			(node_id, bucket, context, dimension, instance, unit, avg_value, min_value, max_value, sample_count)
		SELECT
			node_id,
			date_trunc('day', bucket) AS day_bucket,
			context,
			dimension,
			instance,
			MAX(unit) AS unit,
			SUM(avg_value * sample_count) / NULLIF(SUM(sample_count), 0) AS avg_value,
			MIN(min_value) AS min_value,
			MAX(max_value) AS max_value,
			SUM(sample_count) AS sample_count
		FROM hardware_metric_rollups_1h
		WHERE bucket < $1
		GROUP BY node_id, date_trunc('day', bucket), context, dimension, instance
		ON CONFLICT ON CONSTRAINT uq_rollup_1d
		DO UPDATE SET
			avg_value = EXCLUDED.avg_value,
			min_value = EXCLUDED.min_value,
			max_value = EXCLUDED.max_value,
			sample_count = EXCLUDED.sample_count,
			created_at = now()
	`, before)
	if err != nil {
		return 0, fmt.Errorf("aggregating daily rollups: %w", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteOldRollups removes hourly rollups older than the given duration.
func (s *Store) DeleteOldRollups(ctx context.Context, hourlyRetention, dailyRetention time.Duration) (int64, error) {
	var total int64

	if hourlyRetention > 0 {
		cutoff := time.Now().UTC().Add(-hourlyRetention)
		tag, err := s.pool.Exec(ctx,
			"DELETE FROM hardware_metric_rollups_1h WHERE bucket < $1", cutoff)
		if err != nil {
			return 0, fmt.Errorf("deleting old hourly rollups: %w", err)
		}
		total += tag.RowsAffected()
	}

	if dailyRetention > 0 {
		cutoff := time.Now().UTC().Add(-dailyRetention)
		tag, err := s.pool.Exec(ctx,
			"DELETE FROM hardware_metric_rollups_1d WHERE bucket < $1", cutoff)
		if err != nil {
			return total, fmt.Errorf("deleting old daily rollups: %w", err)
		}
		total += tag.RowsAffected()
	}

	return total, nil
}

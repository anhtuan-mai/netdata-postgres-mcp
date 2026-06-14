-- 003_rollup_tables.sql
-- Adds hourly and daily rollup tables for long-term metric aggregation.
-- A background job in the scheduler populates these from raw samples.

BEGIN;

-- Hourly rollups: one row per node/context/dimension/instance/hour
CREATE TABLE IF NOT EXISTS hardware_metric_rollups_1h (
    node_id     TEXT NOT NULL REFERENCES netdata_nodes(node_id),
    bucket      TIMESTAMPTZ NOT NULL,  -- truncated to hour
    context     TEXT NOT NULL,
    dimension   TEXT NOT NULL,
    instance    TEXT NOT NULL DEFAULT '',
    unit        TEXT NOT NULL DEFAULT '',
    avg_value   DOUBLE PRECISION NOT NULL,
    min_value   DOUBLE PRECISION NOT NULL,
    max_value   DOUBLE PRECISION NOT NULL,
    sample_count INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_rollup_1h UNIQUE (node_id, bucket, context, dimension, instance)
);

CREATE INDEX IF NOT EXISTS idx_rollup_1h_node_time
    ON hardware_metric_rollups_1h (node_id, bucket DESC);

CREATE INDEX IF NOT EXISTS idx_rollup_1h_context_time
    ON hardware_metric_rollups_1h (context, bucket DESC);

-- Daily rollups: one row per node/context/dimension/instance/day
CREATE TABLE IF NOT EXISTS hardware_metric_rollups_1d (
    node_id     TEXT NOT NULL REFERENCES netdata_nodes(node_id),
    bucket      TIMESTAMPTZ NOT NULL,  -- truncated to day
    context     TEXT NOT NULL,
    dimension   TEXT NOT NULL,
    instance    TEXT NOT NULL DEFAULT '',
    unit        TEXT NOT NULL DEFAULT '',
    avg_value   DOUBLE PRECISION NOT NULL,
    min_value   DOUBLE PRECISION NOT NULL,
    max_value   DOUBLE PRECISION NOT NULL,
    sample_count INTEGER NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT uq_rollup_1d UNIQUE (node_id, bucket, context, dimension, instance)
);

CREATE INDEX IF NOT EXISTS idx_rollup_1d_node_time
    ON hardware_metric_rollups_1d (node_id, bucket DESC);

CREATE INDEX IF NOT EXISTS idx_rollup_1d_context_time
    ON hardware_metric_rollups_1d (context, bucket DESC);

COMMIT;

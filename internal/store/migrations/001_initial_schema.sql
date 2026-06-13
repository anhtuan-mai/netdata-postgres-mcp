-- 001_initial_schema.sql
-- Creates the core tables, indexes, and views for netdata-postgres-mcp.

BEGIN;

-- Tracked Netdata nodes
CREATE TABLE IF NOT EXISTS netdata_nodes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id         TEXT UNIQUE NOT NULL,
    hostname        TEXT,
    netdata_base_url TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Raw metric samples collected from Netdata
CREATE TABLE IF NOT EXISTS hardware_metric_samples (
    id              BIGSERIAL PRIMARY KEY,
    node_id         TEXT NOT NULL REFERENCES netdata_nodes(node_id),
    collected_at    TIMESTAMPTZ NOT NULL,
    context         TEXT NOT NULL,
    chart           TEXT,
    family          TEXT,
    instance        TEXT,
    dimension       TEXT NOT NULL,
    unit            TEXT,
    value           DOUBLE PRECISION NOT NULL,
    labels          JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Prevent duplicate samples for the same metric at the same timestamp.
-- coalesce ensures NULLable columns participate in uniqueness.
ALTER TABLE hardware_metric_samples
    ADD CONSTRAINT uq_metric_sample
    UNIQUE (node_id, collected_at, context, dimension,
            COALESCE(chart, ''), COALESCE(instance, ''));

-- Query indexes: time-series lookups by node, context, and combined.
CREATE INDEX IF NOT EXISTS idx_samples_node_time
    ON hardware_metric_samples (node_id, collected_at DESC);

CREATE INDEX IF NOT EXISTS idx_samples_context_time
    ON hardware_metric_samples (context, collected_at DESC);

CREATE INDEX IF NOT EXISTS idx_samples_node_ctx_dim_time
    ON hardware_metric_samples (node_id, context, dimension, collected_at DESC);

-- GIN index for label-based filtering.
CREATE INDEX IF NOT EXISTS idx_samples_labels
    ON hardware_metric_samples USING GIN (labels);

-- Materialized-ish view: latest metric per node/context/dimension/instance.
-- Implemented as a plain view for simplicity; downstream can wrap with
-- materialized view or caching if needed.
CREATE OR REPLACE VIEW hardware_latest_metrics AS
SELECT DISTINCT ON (node_id, context, dimension, COALESCE(instance, ''))
    id,
    node_id,
    collected_at,
    context,
    chart,
    family,
    instance,
    dimension,
    unit,
    value,
    labels
FROM hardware_metric_samples
ORDER BY node_id, context, dimension, COALESCE(instance, ''), collected_at DESC;

COMMIT;

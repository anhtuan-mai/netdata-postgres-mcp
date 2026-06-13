-- 002_optional_timescaledb.sql
-- If TimescaleDB extension is available, convert hardware_metric_samples
-- to a hypertable for efficient time-series storage and queries.
-- This migration is OPTIONAL and safe to skip on plain PostgreSQL.

DO $$
BEGIN
    -- Check if TimescaleDB extension is available
    IF EXISTS (
        SELECT 1 FROM pg_available_extensions WHERE name = 'timescaledb'
    ) THEN
        -- Create extension if not already loaded
        CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE;

        -- Convert to hypertable only if not already one.
        -- TimescaleDB requires the table to have data or be empty.
        IF NOT EXISTS (
            SELECT 1 FROM timescaledb_information.hypertables
            WHERE hypertable_name = 'hardware_metric_samples'
        ) THEN
            PERFORM create_hypertable(
                'hardware_metric_samples',
                'collected_at',
                migrate_data => true,
                if_not_exists => true
            );
            RAISE NOTICE 'Converted hardware_metric_samples to TimescaleDB hypertable';
        END IF;
    ELSE
        RAISE NOTICE 'TimescaleDB not available, skipping hypertable conversion';
    END IF;
END
$$;

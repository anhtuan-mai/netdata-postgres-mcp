-- 004_tenants.sql
-- Adds multi-tenant support with scoped API tokens and row-level policies.

BEGIN;

-- Tenants table
CREATE TABLE IF NOT EXISTS tenants (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- API tokens scoped to tenants
CREATE TABLE IF NOT EXISTS api_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,       -- SHA-256 of the bearer token
    label      TEXT NOT NULL DEFAULT '',    -- human-readable label
    scopes     TEXT[] NOT NULL DEFAULT '{}', -- allowed node_id patterns (empty = all)
    expires_at TIMESTAMPTZ,                -- NULL = never expires
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_hash ON api_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_api_tokens_tenant ON api_tokens (tenant_id);

-- Map nodes to tenants (a node belongs to exactly one tenant)
ALTER TABLE netdata_nodes ADD COLUMN IF NOT EXISTS tenant_id UUID REFERENCES tenants(id);
CREATE INDEX IF NOT EXISTS idx_nodes_tenant ON netdata_nodes (tenant_id);

COMMIT;

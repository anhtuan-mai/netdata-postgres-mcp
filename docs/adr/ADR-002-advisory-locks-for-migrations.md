# ADR-002: Advisory Locks for Database Migrations

## Status

Accepted

## Date

2026-06-14

## Context

The sidecar applies database schema migrations on startup to ensure the required tables, indexes, and functions exist in PostgreSQL. In production environments, multiple sidecar instances may start simultaneously (e.g., during a rolling deployment, container orchestration restart, or horizontal scaling). If two or more instances attempt to run migrations concurrently, this can lead to:

- Race conditions where both instances try to create the same table or index.
- Duplicate migration entries in a tracking table.
- Partial migrations if one instance fails mid-way while another proceeds.
- PostgreSQL errors from conflicting DDL operations.

We need a coordination mechanism to ensure that only one sidecar instance runs migrations at any given time, while other instances wait until the migration is complete before proceeding with startup.

## Decision

We use PostgreSQL advisory locks via `pg_advisory_lock(0x4e444d43)` to serialize migration execution across sidecar instances. The approach works as follows:

1. On startup, before running any migration, the sidecar acquires a session-level advisory lock using a fixed lock key (`0x4e444d43`, which is the hex encoding of "NDMC" — Netdata MCP).
2. If another instance already holds the lock, `pg_advisory_lock` blocks until the lock is released.
3. Once the lock is acquired, the sidecar checks the current schema version and applies any pending migrations.
4. After migrations complete (or if no migrations are needed), the lock is released via `pg_advisory_unlock`.
5. Waiting instances then acquire the lock, find that migrations are already applied, and proceed normally.

## Alternatives Considered

- **File-based locks (e.g., flock)**: Use a lock file on a shared filesystem. Rejected because sidecar instances may run on different hosts or in containers without shared storage, making file-based coordination unreliable.
- **Leader election (e.g., via etcd, Consul, or Kubernetes leader election)**: Elect a single instance to perform migrations. Rejected because it introduces external infrastructure dependencies that the sidecar should not require. The sidecar's only external dependency should be PostgreSQL itself.
- **Optimistic migration with retries**: Attempt migrations and retry on conflict. Rejected because it's fragile, produces noisy error logs, and risks partial application of multi-statement migrations.
- **No coordination**: Rely on PostgreSQL's own DDL locking to prevent conflicts. Rejected because while PostgreSQL serializes some DDL, it does not prevent all race conditions (e.g., two `CREATE TABLE IF NOT EXISTS` calls can both succeed, but concurrent `CREATE INDEX` calls can fail).

## Consequences

**Positive:**

- No external dependencies — the coordination mechanism uses PostgreSQL itself, which the sidecar already connects to.
- Advisory locks are well-understood, reliable, and performant in PostgreSQL.
- Automatically released if the holding session disconnects (crash safety).
- Simple implementation — a single `SELECT pg_advisory_lock(...)` call before migrations and `SELECT pg_advisory_unlock(...)` after.

**Negative:**

- The fixed lock key (`0x4e444d43`) could theoretically collide with another application using the same advisory lock key on the same database. This is extremely unlikely given the key is derived from a project-specific identifier.
- Advisory locks are scoped to a single PostgreSQL cluster. If migrations need to coordinate across multiple database clusters, this approach would not work (not a current requirement).
- Waiting instances block on startup until the lock is released. In pathological cases (e.g., a crashed instance holding a connection open), this could delay startup. Mitigated by PostgreSQL's session disconnect cleanup.

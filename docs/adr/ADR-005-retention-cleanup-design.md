# ADR-005: Retention Cleanup Design

## Status

Accepted

## Date

2026-06-14

## Context

The sidecar continuously collects and stores metrics in PostgreSQL. Without a retention policy, the database will grow unboundedly, eventually consuming all available disk space and degrading query performance. We need an automated mechanism to remove data older than a configurable retention period (e.g., 90 days).

Key requirements:

- **Automatic**: Cleanup should happen without manual intervention.
- **Configurable**: The retention period should be configurable per deployment.
- **Non-disruptive**: Cleanup should not block or significantly slow down ongoing metric collection or MCP tool queries.
- **Simple**: The mechanism should be easy to understand, monitor, and debug.

## Decision

We implement retention cleanup as a daily goroutine within the sidecar's existing scheduler. The design:

1. **Scheduling**: A goroutine runs once every 24 hours (with a configurable interval). It is started alongside the metric collection scheduler during sidecar initialization.
2. **Retention period**: Configurable via `--retention-days` flag or environment variable (default: 90 days).
3. **Cleanup query**: Executes `DELETE FROM metrics WHERE timestamp < NOW() - INTERVAL '...'` against each metrics table. The time cutoff is calculated based on the configured retention period.
4. **Logging**: Each cleanup run logs the number of rows deleted and the duration, providing visibility into the cleanup process.
5. **Graceful shutdown**: The cleanup goroutine listens on the same context cancellation as the rest of the sidecar, stopping cleanly on shutdown.

## Alternatives Considered

- **Separate cleanup process / CronJob**: Run a standalone binary or Kubernetes CronJob that connects to PostgreSQL and deletes old data. Rejected because it introduces a separate deployment artifact that must be configured, scheduled, and monitored independently. The sidecar already has a database connection and scheduler — duplicating this infrastructure is unnecessary.
- **PostgreSQL scheduled job (pg_cron)**: Use the `pg_cron` extension to schedule a `DELETE` or partition drop inside PostgreSQL. Rejected because `pg_cron` is not available in all PostgreSQL deployments (e.g., some managed databases don't support it), and requiring a PostgreSQL extension conflicts with our goal of working with vanilla PostgreSQL.
- **Table partitioning with partition drop**: Partition metrics tables by time (e.g., daily or weekly) and drop entire partitions beyond the retention window. This is the most performant approach for large-scale data, as `DROP PARTITION` is O(1) compared to `DELETE`'s O(n). Rejected for now because it adds significant schema complexity (partition management DDL, migration logic for partition creation) that is not justified at our current data volumes. This decision should be revisited if data volumes grow substantially.
- **PostgreSQL TTL / row-level expiry**: Some PostgreSQL extensions or patterns offer automatic row expiry. Rejected because there is no native PostgreSQL TTL feature, and extension-based solutions (e.g., `pg_partman`) add operational dependencies.

## Consequences

**Positive:**

- Zero additional infrastructure — cleanup runs inside the sidecar process that already exists.
- Simple to understand and debug — a single goroutine with a `DELETE` query and log output.
- Configurable retention period without redeployment (via environment variable).
- Shares the sidecar's graceful shutdown mechanism — no orphaned cleanup processes.

**Negative:**

- `DELETE` is O(n) in the number of rows removed. For very large datasets, a single daily cleanup could delete millions of rows, causing significant I/O and WAL amplification. If data volumes reach this scale, partitioning (the rejected alternative) should be reconsidered.
- The cleanup runs inside the sidecar process. If the sidecar is not running (e.g., during an extended outage), cleanup does not occur and data accumulates. On restart, the first cleanup run may need to delete a large backlog.
- Running cleanup as a goroutine means it shares CPU and memory with metric collection and MCP serving. In practice, the `DELETE` workload is minimal compared to the sidecar's other activities, but it could cause brief latency spikes under extreme conditions.
- Only one sidecar instance should run cleanup if multiple instances connect to the same database. Currently not coordinated — if multiple instances run, they will each execute redundant `DELETE` statements (harmless but wasteful). Could be mitigated with an advisory lock similar to ADR-002 if needed.

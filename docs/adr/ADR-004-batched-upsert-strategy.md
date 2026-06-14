# ADR-004: Batched Upsert Strategy

## Status

Accepted

## Date

2026-06-14

## Context

The sidecar periodically collects metrics from Netdata's API and stores them in PostgreSQL. A single collection cycle can produce hundreds or thousands of metric data points that need to be inserted into the database. The insertion strategy directly impacts:

- **Throughput**: How quickly a collection cycle completes.
- **Database load**: How much work PostgreSQL does per cycle (WAL writes, index updates, lock contention).
- **Duplicate handling**: Metrics may overlap between collection cycles (e.g., if a cycle runs before the previous one's data has aged out), so duplicate rows must be handled gracefully.
- **Atomicity**: A partial insert (some rows written, others not) would leave the database in an inconsistent state for that collection window.

We need an insertion strategy that balances throughput, simplicity, and resource consumption for the expected data volumes (hundreds to low thousands of rows per collection cycle).

## Decision

We use multi-row `INSERT ... ON CONFLICT DO NOTHING` statements with the following parameters:

1. **Batch size**: 500 rows per `INSERT` statement. Each statement inserts up to 500 rows using a single multi-row `VALUES` clause.
2. **Transaction scope**: All batches within a single collection cycle are wrapped in one database transaction. Either all data from the cycle is committed, or none is.
3. **Conflict handling**: `ON CONFLICT DO NOTHING` silently skips rows that would violate a unique constraint (e.g., duplicate metric + timestamp combinations). This makes the insert idempotent.
4. **Parameter binding**: Each row's values are passed as parameterized query arguments (`$1, $2, ...`) to prevent SQL injection and allow PostgreSQL to reuse query plans.

For a collection cycle producing 1,200 rows, this results in 3 `INSERT` statements (500 + 500 + 200) within a single transaction.

## Alternatives Considered

- **COPY protocol (`pgx.CopyFrom`)**: PostgreSQL's fastest bulk loading mechanism. Bypasses the SQL parser entirely and streams rows in a binary format. Rejected because `COPY` does not support `ON CONFLICT` — duplicates would cause the entire `COPY` to fail. Handling this would require a staging table pattern (COPY to temp table, then INSERT ... ON CONFLICT from temp to target), which adds complexity for marginal throughput gains at our data volumes.
- **Individual `INSERT` statements (one per row)**: The simplest approach. Rejected because it generates excessive round-trip overhead and WAL writes. For 1,000 rows, this means 1,000 separate SQL statements and network round-trips instead of 2.
- **Larger batch sizes (e.g., 5,000 or 10,000 rows)**: Fewer statements per cycle. Rejected because PostgreSQL has a limit on the number of query parameters (65,535), and larger batches consume more memory for parameter arrays. A 500-row batch with 5 columns uses 2,500 parameters, well within limits while providing effective batching.
- **Upsert with `ON CONFLICT DO UPDATE`**: Update existing rows instead of skipping them. Rejected because our metrics are immutable once written — a data point for a specific metric at a specific timestamp should not change. `DO NOTHING` is cheaper and semantically correct.

## Consequences

**Positive:**

- Good throughput for expected data volumes — batching reduces round-trips by ~100-500x compared to individual inserts.
- Idempotent inserts via `ON CONFLICT DO NOTHING` — safe to retry or overlap collection windows.
- Full atomicity per collection cycle — no partial data visible to queries.
- Well within PostgreSQL parameter limits at 500-row batches.
- Simpler than a COPY + staging table approach.

**Negative:**

- Not as fast as raw `COPY` for very large datasets. If data volumes grow to tens of thousands of rows per cycle, we may need to revisit this decision.
- The 500-row batch size is a static configuration. An adaptive batch size could optimize for varying data volumes, but adds complexity not justified by current requirements.
- Multi-row `INSERT` statements generate larger SQL strings that PostgreSQL must parse. At 500 rows this is negligible, but would become a concern at much larger batch sizes.

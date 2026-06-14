# ADR-001: Zero-Dependency Prometheus Metrics

## Status

Accepted

## Date

2026-06-14

## Context

The Netdata PostgreSQL MCP sidecar exposes Prometheus-compatible metrics for observability (e.g., collection durations, row counts, error rates). The standard approach in Go is to use the `prometheus/client_golang` library, which provides registries, collectors, and an HTTP handler for the exposition format.

However, the sidecar is designed as a lightweight, single-purpose tool. Pulling in `prometheus/client_golang` introduces a significant dependency tree (including `prometheus/common`, `prometheus/procfs`, protobuf libraries, and others). This conflicts with our goal of keeping the binary small, the dependency surface minimal, and the build fast.

The metrics we need to expose are straightforward: a handful of counters and gauges, all updated from a small number of code paths.

## Decision

We implement Prometheus metrics without using the `prometheus/client_golang` library. Instead, we:

1. Use Go's `sync/atomic` package to maintain metric counters and gauges with lock-free atomic operations.
2. Manually format the `/metrics` HTTP endpoint output to conform to the Prometheus text exposition format (version 0.0.4).
3. Include `# HELP` and `# TYPE` comment lines for each metric to ensure compatibility with Prometheus scrapers and Grafana.

## Alternatives Considered

- **Use `prometheus/client_golang`**: The standard approach. Provides a proven, battle-tested metrics library with built-in process and Go runtime collectors. Rejected because it adds ~15+ transitive dependencies and increases binary size for minimal benefit given our simple metrics surface.
- **Use a lighter metrics library (e.g., `VictoriaMetrics/metrics`)**: A more minimal alternative to the official client. Rejected because even a lightweight library adds an external dependency, and our needs are simple enough to handle in ~100 lines of code.
- **No metrics at all**: Rely on logs for observability. Rejected because structured metrics are far superior for alerting, dashboarding, and integration with existing Netdata/Prometheus monitoring infrastructure.

## Consequences

**Positive:**

- Zero additional dependencies for metrics — the sidecar stays lightweight.
- Full control over the exposition format and what metrics are exposed.
- Atomic operations provide thread-safe metric updates with no lock contention.
- Faster build times and smaller binary size.

**Negative:**

- We must manually maintain the Prometheus text format output, which is error-prone if the format specification changes (unlikely — v0.0.4 has been stable for years).
- No automatic Go runtime metrics (goroutine count, GC stats, memory usage) that `client_golang` provides out of the box. These can be added manually if needed.
- New team members may be surprised by the non-standard approach and need to understand the manual implementation.

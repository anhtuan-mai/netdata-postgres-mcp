# netdata-postgres-mcp

A sidecar service that reads hardware/system metrics from a Netdata Agent or Parent, stores snapshots in PostgreSQL on a configurable interval, and exposes the stored data through an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server for AI assistants.

## Architecture

```mermaid
graph LR
    subgraph Netdata Agent
        A[Collectors] --> B[HTTP API :19999]
    end

    subgraph netdata-postgres-mcp
        C[Collector] -->|GET /api/v3/data| B
        C --> D[PostgreSQL Store]
        E[MCP Server] --> D
    end

    subgraph AI Assistant
        F[Claude / Codex / Cursor] -->|MCP protocol| E
    end

    D --> G[(PostgreSQL)]
```

### Data flow

1. **Collector** queries Netdata's HTTP API every `COLLECTION_INTERVAL_SECONDS` using `/api/v3/data` (JSON, preferred) or `/api/v3/allmetrics?format=prometheus` (fallback).
2. Collected metrics are batch-inserted into PostgreSQL with duplicate-safe upserts.
3. **MCP Server** exposes five tools that AI assistants can call to query, summarize, and analyze the stored metrics.

## Quick start

### Prerequisites

- Go 1.22+ (for building from source)
- PostgreSQL 14+
- A running Netdata Agent (default: `http://localhost:19999`)
- Docker & Docker Compose (optional)

### Option A: Docker Compose

```bash
cd contrib/netdata-postgres-mcp
cp .env.example .env
# Edit .env — set NETDATA_BASE_URL to your Netdata agent

docker compose up -d
```

This starts PostgreSQL, runs migrations automatically, and begins collecting metrics.

### Option B: Build from source

```bash
cd contrib/netdata-postgres-mcp
go build -o netdata-postgres-mcp ./cmd/netdata-postgres-mcp

# Set required config
export POSTGRES_DSN="postgres://user:pass@localhost:5432/netdata_metrics?sslmode=disable"
export NETDATA_BASE_URL="http://localhost:19999"

# Run migrations
./netdata-postgres-mcp migrate

# Collect once (test)
./netdata-postgres-mcp collect-once

# Run continuously (scheduler + MCP HTTP server)
./netdata-postgres-mcp run
```

## Configuration

All configuration can be set via YAML file and/or environment variables. Environment variables take precedence.

| Environment Variable | YAML Key | Default | Description |
|---|---|---|---|
| `NETDATA_BASE_URL` | `netdata_base_url` | `http://localhost:19999` | Netdata agent URL |
| `NETDATA_API_KEY` | `netdata_api_key` | _(empty)_ | API key for authenticated Netdata |
| `POSTGRES_DSN` | `postgres_dsn` | _(required)_ | PostgreSQL connection string |
| `COLLECTION_INTERVAL_SECONDS` | `collection_interval_seconds` | `60` | Seconds between collections |
| `NODE_ID` | `node_id` | _(auto-detect)_ | Node identifier |
| `ENABLED_CONTEXTS` | `enabled_contexts` | see below | Comma-separated metric contexts |
| `MCP_BIND_ADDR` | `mcp_bind_addr` | `127.0.0.1:8765` | MCP HTTP/SSE server address |
| `LOG_LEVEL` | `log_level` | `info` | Log level: debug, info, warn, error |
| `CONFIG_FILE` | — | _(none)_ | Path to YAML config file |

**Default enabled contexts:** `system.cpu`, `system.ram`, `system.swap`, `system.io`, `system.pgpgio`, `system.ip`, `disk.io`, `disk.ops`, `disk.util`, `disk.space`, `disk.inodes`, `apps.cpu`, `apps.mem`

## PostgreSQL schema

### `netdata_nodes`

Registered Netdata nodes with metadata.

| Column | Type | Description |
|---|---|---|
| `id` | `UUID` | Primary key |
| `node_id` | `TEXT UNIQUE` | Logical node identifier |
| `hostname` | `TEXT` | OS hostname |
| `netdata_base_url` | `TEXT` | Agent URL |
| `created_at` | `TIMESTAMPTZ` | Row creation time |
| `updated_at` | `TIMESTAMPTZ` | Last update time |

### `hardware_metric_samples`

Raw metric data points collected from Netdata.

| Column | Type | Description |
|---|---|---|
| `id` | `BIGSERIAL` | Primary key |
| `node_id` | `TEXT` | FK to `netdata_nodes.node_id` |
| `collected_at` | `TIMESTAMPTZ` | When the metric was sampled |
| `context` | `TEXT` | Netdata context (e.g., `system.cpu`) |
| `chart` | `TEXT` | Chart name |
| `family` | `TEXT` | Chart family |
| `instance` | `TEXT` | Instance (e.g., disk name, mount point) |
| `dimension` | `TEXT` | Metric dimension (e.g., `user`, `system`) |
| `unit` | `TEXT` | Unit of measurement |
| `value` | `DOUBLE PRECISION` | Metric value |
| `labels` | `JSONB` | Additional labels |

**Indexes:** `(node_id, collected_at DESC)`, `(context, collected_at DESC)`, `(node_id, context, dimension, collected_at DESC)`, GIN on `labels`.

**Unique constraint:** `(node_id, collected_at, context, dimension, COALESCE(chart,''), COALESCE(instance,''))` — prevents duplicate inserts.

### `hardware_latest_metrics` (view)

Latest metric per `node_id/context/dimension/instance` for quick dashboard-style queries.

### TimescaleDB (optional)

If TimescaleDB is installed, migration `002_optional_timescaledb.sql` converts `hardware_metric_samples` to a hypertable. The service works on plain PostgreSQL without it.

## Netdata API requests

The collector queries these endpoints:

**Primary — JSON data API:**
```
GET /api/v3/data?contexts=system.cpu,system.ram,...&after=-60&points=1&time_group=avg&group_by=dimension,node,instance&format=json
```

**Fallback — Prometheus format:**
```
GET /api/v3/allmetrics?format=prometheus&source=average
```

## CLI commands

| Command | Description |
|---|---|
| `netdata-postgres-mcp migrate` | Run database migrations |
| `netdata-postgres-mcp collect-once` | Collect metrics once and exit |
| `netdata-postgres-mcp run` | Start scheduler + MCP HTTP/SSE server |
| `netdata-postgres-mcp mcp` | Start MCP server on stdio (for direct AI connection) |

## MCP tools

### `list_nodes`
List all monitored nodes with last collection timestamp.

### `latest_hardware_metrics`
Return latest CPU/RAM/disk metrics for a node, grouped by context/instance/dimension.

**Parameters:** `node_id` (optional), `contexts` (optional, comma-separated)

### `query_hardware_metrics`
Query historical metrics with flexible filtering.

**Parameters:** `node_id`, `context`, `dimension`, `instance`, `after` (ISO or relative like `-1h`), `before`, `limit` (default 500)

### `summarize_hardware_performance`
Summarize CPU/RAM/disk averages, peaks, top abnormal dimensions, and generate a human-readable summary.

**Parameters:** `node_id` (required), `after` (default `-1h`), `before`

### `find_hardware_bottlenecks`
Detect CPU/RAM/disk bottlenecks with confidence scores and supporting evidence.

**Parameters:** `node_id` (required), `after` (default `-15m`)

## Connecting AI assistants

### Claude Desktop / Claude Code

Add to your MCP configuration (`~/.claude/claude_desktop_config.json` or project `.mcp.json`):

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "command": "/path/to/netdata-postgres-mcp",
      "args": ["mcp"],
      "env": {
        "POSTGRES_DSN": "postgres://netdata:netdata@localhost:5432/netdata_metrics?sslmode=disable"
      }
    }
  }
}
```

### HTTP/SSE transport (Cursor, remote clients)

Start the service with `netdata-postgres-mcp run`, then connect to:
```
http://127.0.0.1:8765/sse
```

### Cursor

In Cursor settings, add an MCP server pointing to the SSE endpoint above.

## Example prompts

Once connected, try these with your AI assistant:

- "Show latest CPU/RAM/disk status for node my-server-01"
- "Find hardware bottlenecks in the last 30 minutes"
- "Compare disk performance between node A and node B"
- "What was the peak CPU usage yesterday between 2pm and 4pm?"
- "Summarize hardware performance for the last 24 hours"
- "Are there any disk I/O bottlenecks right now?"

## Running tests

```bash
cd contrib/netdata-postgres-mcp
go test ./...
```

## Troubleshooting

### "postgres_dsn is required"
Set the `POSTGRES_DSN` environment variable or add `postgres_dsn` to your config file.

### No metrics collected
1. Verify Netdata is running: `curl http://localhost:19999/api/v1/info`
2. Check the configured `NETDATA_BASE_URL` is reachable from the sidecar.
3. Try the Prometheus fallback endpoint directly: `curl 'http://localhost:19999/api/v3/allmetrics?format=prometheus' | head`

### MCP connection issues
- **stdio mode** (`mcp` command): ensure the binary path and `POSTGRES_DSN` are correct in your MCP client config.
- **HTTP/SSE mode** (`run` command): verify `MCP_BIND_ADDR` is accessible. For Docker, use `0.0.0.0:8765` and map the port.

### Duplicate insert warnings
These are normal — the unique constraint prevents storing the same metric twice for the same timestamp. Duplicates are silently skipped via `ON CONFLICT DO NOTHING`.

### High memory usage
Reduce the number of enabled contexts or increase the collection interval. Each context can generate many dimensions (one per disk, per mount point, per app, etc.).

## License

GPL-3.0-or-later — same as the Netdata Agent.

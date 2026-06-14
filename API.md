# API Reference

netdata-postgres-mcp exposes two interfaces: **MCP tools** for AI assistants and **HTTP endpoints** for health/observability.

## HTTP Endpoints

These endpoints are served on `MCP_BIND_ADDR` (default `127.0.0.1:8765`). Health and metrics endpoints are **not** protected by auth middleware.

### GET /healthz

Liveness probe. Returns 200 if the process is alive.

**Response:**
```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

### GET /readyz

Readiness probe. Pings PostgreSQL and returns 200 if reachable, 503 otherwise.

**Response (ready):**
```json
{
  "status": "ready"
}
```

**Response (not ready):**
```json
{
  "status": "error",
  "reason": "database unreachable"
}
```

### GET /metrics

Prometheus-compatible self-observability metrics in exposition format.

**Response (text/plain):**
```
# HELP netdata_mcp_collections_total Total number of collection cycles.
# TYPE netdata_mcp_collections_total counter
netdata_mcp_collections_total 142

# HELP netdata_mcp_collection_errors_total Total number of failed collection cycles.
# TYPE netdata_mcp_collection_errors_total counter
netdata_mcp_collection_errors_total 3

# HELP netdata_mcp_samples_inserted_total Total number of metric samples inserted into PostgreSQL.
# TYPE netdata_mcp_samples_inserted_total counter
netdata_mcp_samples_inserted_total 28400

# HELP netdata_mcp_retention_deleted_total Total number of expired samples deleted by retention cleanup.
# TYPE netdata_mcp_retention_deleted_total counter
netdata_mcp_retention_deleted_total 0

# HELP netdata_mcp_collection_duration_seconds Duration of the last collection cycle in seconds.
# TYPE netdata_mcp_collection_duration_seconds gauge
netdata_mcp_collection_duration_seconds{stat="last"} 1.234567
netdata_mcp_collection_duration_seconds{stat="avg"} 0.987654
netdata_mcp_collection_duration_seconds{stat="count"} 142
```

### GET /sse

Server-Sent Events endpoint for MCP protocol transport. Clients connect here to establish an SSE stream for receiving MCP responses.

### POST /message

MCP message endpoint. Clients send JSON-RPC tool call requests here.

---

## MCP Tools

All tools return JSON-formatted text content via the MCP protocol. Each tool handler has a 30-second timeout for database queries.

### list_nodes

List all monitored Netdata nodes with their last collection timestamp.

**Parameters:** None

**Response:**
```json
[
  {
    "node_id": "web-server-01",
    "hostname": "web-server-01.prod",
    "netdata_base_url": "http://10.0.1.10:19999",
    "last_collected_at": "2026-06-14T10:30:00Z"
  },
  {
    "node_id": "db-server-01",
    "hostname": "db-server-01.prod",
    "netdata_base_url": "http://10.0.1.20:19999",
    "last_collected_at": "2026-06-14T10:29:55Z"
  }
]
```

**Notes:**
- `last_collected_at` is null if no metrics have been collected yet
- Nodes are ordered by `node_id`

---

### latest_hardware_metrics

Return the latest CPU, RAM, and disk metrics for a node, grouped by context/instance/dimension.

**Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node_id` | string | No | _(all nodes)_ | Node ID to query |
| `contexts` | string | No | _(all contexts)_ | Comma-separated list of contexts to filter (e.g. `system.cpu,system.ram`) |

**Response:**
```json
[
  {
    "node_id": "web-server-01",
    "collected_at": "2026-06-14T10:30:00Z",
    "context": "system.cpu",
    "chart": "system.cpu",
    "family": "cpu",
    "instance": "",
    "dimension": "user",
    "unit": "percentage",
    "value": 23.45
  },
  {
    "node_id": "web-server-01",
    "collected_at": "2026-06-14T10:30:00Z",
    "context": "system.ram",
    "chart": "system.ram",
    "family": "ram",
    "dimension": "used",
    "unit": "MiB",
    "value": 4096.0
  }
]
```

**Notes:**
- Uses the `hardware_latest_metrics` view which returns the most recent value per node/context/dimension/instance
- Results are ordered by context, then dimension

---

### query_hardware_metrics

Query historical hardware metrics with flexible filtering and time ranges.

**Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node_id` | string | No | _(all nodes)_ | Filter by node ID |
| `context` | string | No | _(all contexts)_ | Filter by metric context (e.g. `system.cpu`) |
| `dimension` | string | No | _(all dimensions)_ | Filter by dimension (e.g. `user`, `system`) |
| `instance` | string | No | _(all instances)_ | Filter by instance (e.g. disk name, mount point) |
| `after` | string | No | _(no lower bound)_ | Start time. ISO 8601 timestamp or relative duration (`-1h`, `-30m`, `-24h`) |
| `before` | string | No | _(now)_ | End time. ISO 8601 timestamp |
| `limit` | number | No | `500` | Maximum number of results (capped at 5000) |

**Response:**
```json
[
  {
    "node_id": "web-server-01",
    "collected_at": "2026-06-14T10:30:00Z",
    "context": "system.cpu",
    "chart": "system.cpu",
    "family": "cpu",
    "instance": "",
    "dimension": "user",
    "unit": "percentage",
    "value": 23.45
  }
]
```

**Notes:**
- Results are ordered by `collected_at DESC`
- The `after` parameter supports relative durations like `-1h` (one hour ago), `-30m`, `-7d`
- Limit is silently capped at 5000 to prevent excessive memory usage

---

### summarize_hardware_performance

Generate a performance summary for a node over a time window with CPU, RAM, and disk averages, peaks, and a human-readable summary.

**Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node_id` | string | **Yes** | — | Node ID to summarize |
| `after` | string | No | `-1h` | Start of time window. Relative duration or ISO timestamp |
| `before` | string | No | _(now)_ | End of time window. ISO timestamp |

**Response:**
```json
{
  "node_id": "web-server-01",
  "time_range": {
    "after": "2026-06-14T09:30:00Z",
    "before": "2026-06-14T10:30:00Z"
  },
  "cpu": {
    "avg_total": 34.5,
    "peak_total": 89.2,
    "breakdown": {
      "user": {"avg": 23.1, "max": 67.8},
      "system": {"avg": 8.4, "max": 21.4},
      "iowait": {"avg": 3.0, "max": 45.0}
    }
  },
  "ram": {
    "used_avg_mib": 4096,
    "used_peak_mib": 6144
  },
  "disk": {
    "busiest_device": "sda",
    "peak_utilization": 78.5
  },
  "top_abnormal": [
    {"context": "system.cpu", "dimension": "iowait", "reason": "high variance", "max": 45.0, "avg": 3.0}
  ],
  "summary": "CPU averaged 34.5% with a peak of 89.2%. RAM usage stable around 4 GiB. Disk sda showed elevated I/O wait."
}
```

**Notes:**
- The `summary` field is a human-readable text paragraph suitable for AI assistant responses
- `top_abnormal` highlights dimensions with unusual patterns (high variance, sustained peaks)

---

### find_hardware_bottlenecks

Detect CPU, RAM, and disk bottlenecks from stored metrics with confidence scores and supporting evidence.

**Parameters:**

| Name | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `node_id` | string | **Yes** | — | Node ID to analyze |
| `after` | string | No | `-15m` | Start of analysis window. Relative duration or ISO timestamp |

**Response:**
```json
{
  "node_id": "web-server-01",
  "analysis_window": "-15m",
  "bottlenecks": [
    {
      "type": "cpu",
      "confidence": 0.85,
      "description": "High CPU utilization detected",
      "evidence": [
        {"metric": "system.cpu.user", "avg": 78.5, "max": 98.2},
        {"metric": "system.cpu.system", "avg": 12.3, "max": 25.1}
      ],
      "recommendation": "Consider scaling CPU resources or identifying CPU-intensive processes via apps.cpu"
    },
    {
      "type": "disk_io",
      "confidence": 0.62,
      "description": "Elevated disk I/O wait",
      "evidence": [
        {"metric": "system.cpu.iowait", "avg": 8.5, "max": 45.0},
        {"metric": "disk.util.sda", "avg": 72.0, "max": 99.8}
      ],
      "recommendation": "Check for heavy write workloads or consider faster storage"
    }
  ]
}
```

**Notes:**
- Confidence scores range from 0.0 to 1.0
- Returns an empty `bottlenecks` array when no issues are detected
- Bottleneck types: `cpu`, `ram`, `disk_io`, `disk_space`, `network`

---

## Authentication

When `MCP_AUTH_TOKEN` is set, the `/sse` and `/message` endpoints require authentication. Health endpoints (`/healthz`, `/readyz`) and metrics (`/metrics`) are never auth-protected.

**Header authentication:**
```
Authorization: Bearer <token>
```

**Query parameter authentication** (for SSE clients that can't set headers):
```
GET /sse?token=<token>
```

**401 Unauthorized response:**
```json
{
  "error": "unauthorized — provide Authorization: Bearer <token>"
}
```

## Rate Limiting

When `RATE_LIMIT_RPS` is set, requests are rate-limited per client IP using a token bucket algorithm.

**429 Too Many Requests response:**
```json
{
  "error": "rate limit exceeded — try again later"
}
```

The `Retry-After: 1` header is included in rate-limited responses.

## Error Handling

All MCP tools return errors as text content rather than throwing exceptions. This allows AI assistants to see and reason about errors.

**Example error response (MCP tool):**
```json
{
  "content": [
    {
      "type": "text",
      "text": "Error querying metrics: context deadline exceeded"
    }
  ]
}
```

# Installation Guide

Deploy `netdata-postgres-mcp` on your server or VM. Choose the method that fits your environment.

---

## Prerequisites

- **PostgreSQL 14+** (local or remote)
- **Netdata Agent** running on the target machine (default: `http://localhost:19999`)
- Network connectivity between the sidecar, PostgreSQL, and Netdata

---

## Method 1: Pre-built Binary (Recommended)

Download the latest release binary — no build tools required.

### Linux (amd64)

```bash
# Download binary and checksum
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-linux-amd64
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-linux-amd64.sha256

# Verify integrity
sha256sum -c netdata-postgres-mcp-linux-amd64.sha256

# Install
chmod +x netdata-postgres-mcp-linux-amd64
sudo mv netdata-postgres-mcp-linux-amd64 /usr/local/bin/netdata-postgres-mcp
```

### Linux (arm64)

```bash
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-linux-arm64
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-linux-arm64.sha256
sha256sum -c netdata-postgres-mcp-linux-arm64.sha256
chmod +x netdata-postgres-mcp-linux-arm64
sudo mv netdata-postgres-mcp-linux-arm64 /usr/local/bin/netdata-postgres-mcp
```

### macOS (Apple Silicon)

```bash
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-darwin-arm64
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-darwin-arm64.sha256
shasum -a 256 -c netdata-postgres-mcp-darwin-arm64.sha256
chmod +x netdata-postgres-mcp-darwin-arm64
sudo mv netdata-postgres-mcp-darwin-arm64 /usr/local/bin/netdata-postgres-mcp
```

### macOS (Intel)

```bash
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-darwin-amd64
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-darwin-amd64.sha256
shasum -a 256 -c netdata-postgres-mcp-darwin-amd64.sha256
chmod +x netdata-postgres-mcp-darwin-amd64
sudo mv netdata-postgres-mcp-darwin-amd64 /usr/local/bin/netdata-postgres-mcp
```

### Windows

```powershell
# Download
Invoke-WebRequest -Uri "https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-windows-amd64.exe" -OutFile "$env:LOCALAPPDATA\netdata-postgres-mcp.exe"

# Add to PATH (user-level)
$path = [Environment]::GetEnvironmentVariable("PATH", "User")
if ($path -notlike "*$env:LOCALAPPDATA*") {
    [Environment]::SetEnvironmentVariable("PATH", "$path;$env:LOCALAPPDATA", "User")
}
```

### Verify installation

```bash
netdata-postgres-mcp version
```

---

## Method 2: Docker Compose

The fastest way to get everything running together.

```bash
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp

# Configure
cp .env.example .env
```

Edit `.env` — at minimum set `NETDATA_BASE_URL` to your Netdata agent:

```bash
# .env
NETDATA_BASE_URL=http://localhost:19999
POSTGRES_DSN=postgres://netdata:netdata@postgres:5432/netdata_metrics?sslmode=disable
MCP_BIND_ADDR=0.0.0.0:8765
LOG_LEVEL=info
```

Start the stack:

```bash
docker compose up -d
```

This starts PostgreSQL + the sidecar. Migrations run automatically on startup.

Verify:

```bash
curl http://localhost:8765/healthz
# {"status":"ok","version":"..."}

curl http://localhost:8765/readyz
# {"status":"ready"}
```

---

## Method 3: Docker (Standalone)

When you already have PostgreSQL running elsewhere.

```bash
docker run -d \
  --name netdata-postgres-mcp \
  --restart unless-stopped \
  -e POSTGRES_DSN="postgres://netdata:password@your-pg-host:5432/netdata_metrics?sslmode=disable" \
  -e NETDATA_BASE_URL="http://host.docker.internal:19999" \
  -e MCP_BIND_ADDR="0.0.0.0:8765" \
  -p 8765:8765 \
  ghcr.io/anhtuan-mai/netdata-postgres-mcp:latest run
```

On Linux, use `--network host` or the actual host IP instead of `host.docker.internal`.

---

## Method 4: Build from Source

Requires Go 1.22+.

```bash
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp
make build
sudo mv netdata-postgres-mcp /usr/local/bin/
```

---

## Method 5: Helm (Kubernetes)

```bash
# From the repo
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp

helm install netdata-mcp ./helm/netdata-postgres-mcp \
  --set env.NETDATA_BASE_URL=http://netdata:19999 \
  --set secret.POSTGRES_DSN="postgres://netdata:password@postgres:5432/netdata_metrics?sslmode=disable"
```

Or with a values file:

```yaml
# my-values.yaml
env:
  NETDATA_BASE_URL: "http://netdata:19999"
  COLLECTION_INTERVAL_SECONDS: "60"
  LOG_LEVEL: "info"

secret:
  POSTGRES_DSN: "postgres://netdata:password@postgres:5432/netdata_metrics?sslmode=disable"
  MCP_AUTH_TOKEN: "your-secret-token"

resources:
  requests:
    cpu: 100m
    memory: 64Mi
  limits:
    cpu: 500m
    memory: 256Mi
```

```bash
helm install netdata-mcp ./helm/netdata-postgres-mcp -f my-values.yaml
```

---

## Post-Install Setup

### 1. Prepare PostgreSQL

Create a dedicated database and user if you haven't already:

```sql
CREATE USER netdata WITH PASSWORD 'your-secure-password';
CREATE DATABASE netdata_metrics OWNER netdata;
GRANT ALL PRIVILEGES ON DATABASE netdata_metrics TO netdata;
```

### 2. Run Migrations

Migrations are automatic when using `run`, but you can run them explicitly:

```bash
export POSTGRES_DSN="postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable"
netdata-postgres-mcp migrate
```

### 3. Verify Netdata Connectivity

```bash
curl -s http://localhost:19999/api/v1/info | head -5
```

If this fails, install or start the Netdata agent:

```bash
# Linux one-liner
curl https://get.netdata.cloud/kickstart.sh > /tmp/netdata-kickstart.sh
sh /tmp/netdata-kickstart.sh --stable-channel
```

### 4. Test Collection

```bash
export POSTGRES_DSN="postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable"
export NETDATA_BASE_URL="http://localhost:19999"

# Single collection test
netdata-postgres-mcp collect-once
```

### 5. Start the Service

```bash
netdata-postgres-mcp run
```

This starts both the metric collector (on interval) and the MCP HTTP/SSE server.

---

## Running as a System Service

### Linux (systemd)

```bash
sudo tee /etc/systemd/system/netdata-postgres-mcp.service > /dev/null <<'EOF'
[Unit]
Description=Netdata PostgreSQL MCP Sidecar
After=network-online.target postgresql.service netdata.service
Wants=network-online.target

[Service]
Type=simple
User=netdata
Group=netdata
Environment=POSTGRES_DSN=postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable
Environment=NETDATA_BASE_URL=http://localhost:19999
Environment=MCP_BIND_ADDR=127.0.0.1:8765
Environment=LOG_LEVEL=info
Environment=RETENTION_DAYS=30
ExecStart=/usr/local/bin/netdata-postgres-mcp run
Restart=always
RestartSec=10

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now netdata-postgres-mcp
```

Check status:

```bash
sudo systemctl status netdata-postgres-mcp
sudo journalctl -u netdata-postgres-mcp -f
```

### Windows (NSSM)

```powershell
# Download NSSM
Invoke-WebRequest -Uri "https://nssm.cc/release/nssm-2.24.zip" -OutFile "$env:TEMP\nssm.zip"
Expand-Archive "$env:TEMP\nssm.zip" -DestinationPath "C:\nssm" -Force

# Install service
C:\nssm\nssm-2.24\win64\nssm.exe install NetdataPostgresMCP "$env:LOCALAPPDATA\netdata-postgres-mcp.exe" "run"
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppEnvironmentExtra `
    "POSTGRES_DSN=postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable" `
    "NETDATA_BASE_URL=http://localhost:19999" `
    "MCP_BIND_ADDR=127.0.0.1:8765" `
    "LOG_LEVEL=info"

# Log output
New-Item -ItemType Directory -Force -Path "C:\netdata-postgres-mcp\logs"
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppStdout "C:\netdata-postgres-mcp\logs\stdout.log"
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppStderr "C:\netdata-postgres-mcp\logs\stderr.log"

# Start
Start-Service NetdataPostgresMCP
Get-Service NetdataPostgresMCP
```

### macOS (launchd)

```bash
sudo tee /Library/LaunchDaemons/com.netdata-postgres-mcp.plist > /dev/null <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.netdata-postgres-mcp</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/netdata-postgres-mcp</string>
        <string>run</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>POSTGRES_DSN</key>
        <string>postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable</string>
        <key>NETDATA_BASE_URL</key>
        <string>http://localhost:19999</string>
        <key>MCP_BIND_ADDR</key>
        <string>127.0.0.1:8765</string>
        <key>LOG_LEVEL</key>
        <string>info</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/netdata-postgres-mcp.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/netdata-postgres-mcp.log</string>
</dict>
</plist>
EOF

sudo launchctl load /Library/LaunchDaemons/com.netdata-postgres-mcp.plist
```

---

## Configuration Reference

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `POSTGRES_DSN` | _(required)_ | PostgreSQL connection string |
| `NETDATA_BASE_URL` | `http://localhost:19999` | Netdata agent URL |
| `NETDATA_API_KEY` | _(empty)_ | API key for authenticated Netdata |
| `COLLECTION_INTERVAL_SECONDS` | `60` | Collection frequency |
| `NODE_ID` | _(auto-detect)_ | Unique node identifier |
| `ENABLED_CONTEXTS` | see below | Comma-separated metric contexts |
| `MCP_BIND_ADDR` | `127.0.0.1:8765` | HTTP/SSE server bind address |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `text` | `text` or `json` |
| `RETENTION_DAYS` | `30` | Auto-delete old samples (0 = disable) |
| `MCP_AUTH_TOKEN` | _(empty)_ | Bearer token for MCP endpoints |
| `RATE_LIMIT_RPS` | `0` | Max requests/sec per IP (0 = disabled) |
| `TLS_CERT_FILE` | _(empty)_ | TLS certificate path |
| `TLS_KEY_FILE` | _(empty)_ | TLS private key path |
| `CONFIG_FILE` | _(none)_ | Path to YAML config file |

**Default enabled contexts:** `system.cpu`, `system.ram`, `system.swap`, `system.io`, `system.pgpgio`, `system.ip`, `disk.io`, `disk.ops`, `disk.util`, `disk.space`, `disk.inodes`, `apps.cpu`, `apps.mem`

### YAML Config File

For complex setups, use a YAML config file (see `config.example.yaml` in the repo):

```yaml
postgres_dsn: "postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable"
netdata_base_url: "http://localhost:19999"
node_id: "my-server-01"
collection_interval_seconds: 60
retention_days: 30
mcp_bind_addr: "127.0.0.1:8765"
log_level: info

enabled_contexts:
  - system.cpu
  - system.ram
  - system.swap
  - disk.space

pool:
  min_conns: 2
  max_conns: 10
  max_conn_lifetime: 30m
  max_conn_idle_time: 5m
  health_check_period: 30s
```

Load with:

```bash
export CONFIG_FILE=/etc/netdata-postgres-mcp/config.yaml
netdata-postgres-mcp run
```

Environment variables override YAML values.

---

## Connecting AI Assistants

### Claude Desktop / Claude Code

Add to your MCP configuration (`~/.claude/claude_desktop_config.json` or project `.mcp.json`):

**HTTP/SSE (recommended for remote servers):**

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "url": "http://your-server:8765/sse"
    }
  }
}
```

**Stdio (local only, no HTTP server needed):**

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "command": "/usr/local/bin/netdata-postgres-mcp",
      "args": ["mcp"],
      "env": {
        "POSTGRES_DSN": "postgres://netdata:password@localhost:5432/netdata_metrics?sslmode=disable"
      }
    }
  }
}
```

### Cursor

In Cursor Settings → MCP Servers, add an SSE server:

```
http://your-server:8765/sse
```

### With Authentication

If `MCP_AUTH_TOKEN` is set, clients must provide the token:

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "url": "http://your-server:8765/sse?token=your-secret-token"
    }
  }
}
```

---

## Health Checks & Monitoring

### Endpoints

| Endpoint | Description |
|---|---|
| `GET /healthz` | Liveness probe — always returns 200 if process is alive |
| `GET /readyz` | Readiness — verifies PostgreSQL connection |
| `GET /metrics` | Prometheus metrics |

### Prometheus Scraping

```yaml
# prometheus.yml
scrape_configs:
  - job_name: netdata-mcp
    static_configs:
      - targets: ['localhost:8765']
```

### Grafana Dashboard

Import `grafana/dashboard.json` from the repo for a pre-built dashboard showing collection rates, errors, and latency.

### Alerting

Copy `prometheus/alerts.yml` from the repo for pre-built alert rules:
- High collection error rate (>20%)
- All collections failing
- No collections for 10 minutes
- Slow/very slow collection cycles
- No samples inserted

---

## Production Hardening

### Enable Authentication

```bash
export MCP_AUTH_TOKEN="$(openssl rand -hex 32)"
```

### Enable TLS

```bash
export TLS_CERT_FILE=/etc/ssl/certs/mcp.crt
export TLS_KEY_FILE=/etc/ssl/private/mcp.key
```

### Rate Limiting

```bash
export RATE_LIMIT_RPS=100
```

### JSON Logging (for log aggregation)

```bash
export LOG_FORMAT=json
```

### Bind to Localhost Only

If the MCP server should only be accessed locally or via a reverse proxy:

```bash
export MCP_BIND_ADDR=127.0.0.1:8765
```

### Firewall

Only open port 8765 if remote AI assistants need direct access:

```bash
# Ubuntu/Debian
sudo ufw allow 8765/tcp

# RHEL/Rocky
sudo firewall-cmd --permanent --add-port=8765/tcp
sudo firewall-cmd --reload
```

---

## Upgrading

### Binary

```bash
# Download new version
curl -LO https://github.com/anhtuan-mai/netdata-postgres-mcp/releases/latest/download/netdata-postgres-mcp-linux-amd64

# Replace binary
sudo systemctl stop netdata-postgres-mcp
sudo mv netdata-postgres-mcp-linux-amd64 /usr/local/bin/netdata-postgres-mcp
sudo chmod +x /usr/local/bin/netdata-postgres-mcp
sudo systemctl start netdata-postgres-mcp
```

Migrations run automatically on startup — no manual migration step needed.

### Docker

```bash
docker compose pull
docker compose up -d
```

### Helm

```bash
helm upgrade netdata-mcp ./helm/netdata-postgres-mcp -f my-values.yaml
```

---

## Troubleshooting

### Service won't start

```bash
# Check logs
sudo journalctl -u netdata-postgres-mcp --since "5 minutes ago"

# Common causes:
# - POSTGRES_DSN not set or invalid
# - PostgreSQL not reachable (firewall, wrong host/port)
# - Port 8765 already in use
```

### "postgres_dsn is required"

The `POSTGRES_DSN` environment variable is not set. Set it or provide a config file with `postgres_dsn`.

### No metrics collected

1. Verify Netdata is running: `curl http://localhost:19999/api/v1/info`
2. Check `NETDATA_BASE_URL` is correct
3. Try a manual collect: `netdata-postgres-mcp collect-once`
4. Check logs for error details

### Cannot connect to MCP server

- Verify the service is running: `curl http://localhost:8765/healthz`
- If accessing remotely, ensure `MCP_BIND_ADDR` is `0.0.0.0:8765` (not `127.0.0.1`)
- Check firewall rules allow port 8765
- If using auth, ensure the token is correct

### High memory usage

Reduce the number of enabled contexts or increase the collection interval:

```bash
export ENABLED_CONTEXTS="system.cpu,system.ram,disk.space"
export COLLECTION_INTERVAL_SECONDS=120
```

### Database growing too large

Reduce retention or enable TimescaleDB compression:

```bash
export RETENTION_DAYS=7
```

---

## What's Next

- **Multi-node deployment** — See [DEPLOYMENT.md](DEPLOYMENT.md) for collecting metrics from multiple servers into a single database
- **API reference** — See [API.md](API.md) for all HTTP endpoints and MCP tools
- **Contributing** — See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup

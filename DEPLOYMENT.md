# Multi-Node Deployment Guide

Deploy `netdata-postgres-mcp` across multiple nodes (Ubuntu, RHEL, Windows Server) with all metrics flowing to a **centralized PostgreSQL server**.

## Architecture Overview

```
┌──────────────────┐     ┌──────────────────┐     ┌──────────────────────┐
│  Ubuntu Node     │     │  RHEL Node       │     │  Windows Server Node │
│                  │     │                  │     │                      │
│  Netdata Agent   │     │  Netdata Agent   │     │  Netdata Agent       │
│  (:19999)        │     │  (:19999)        │     │  (:19999)            │
│       │          │     │       │          │     │       │              │
│  MCP Sidecar     │     │  MCP Sidecar     │     │  MCP Sidecar         │
│  (collector)     │     │  (collector)     │     │  (collector)         │
│       │          │     │       │          │     │       │              │
└───────┼──────────┘     └───────┼──────────┘     └───────┼──────────────┘
        │                        │                        │
        └────────────────┬───────┘────────────────────────┘
                         │
                         ▼
              ┌─────────────────────┐
              │  Central PostgreSQL  │
              │  Server             │
              │  (:5432)            │
              │                     │
              │  + MCP Server       │
              │  (:8765)            │
              └─────────────────────┘
                         │
                         ▼
              ┌─────────────────────┐
              │  AI Assistant       │
              │  (Claude / Cursor)  │
              └─────────────────────┘
```

Each node runs:
1. **Netdata Agent** — collects system metrics locally (CPU, RAM, disk, network)
2. **netdata-postgres-mcp sidecar** (collector only) — reads from the local agent and writes to the central PostgreSQL

The central server runs:
1. **PostgreSQL** — stores all metrics from every node
2. **netdata-postgres-mcp** (MCP server mode) — serves the aggregated data to AI assistants

---

## Phase 1: Central PostgreSQL Server Setup

### Option A: Dedicated Linux Server (Recommended for Production)

#### Ubuntu / Debian

```bash
# Install PostgreSQL 16
sudo apt update
sudo apt install -y postgresql-16 postgresql-client-16

# Start and enable
sudo systemctl enable --now postgresql

# Create database and user
sudo -u postgres psql <<'SQL'
CREATE USER netdata WITH PASSWORD 'CHANGE_ME_STRONG_PASSWORD';
CREATE DATABASE netdata_metrics OWNER netdata;
GRANT ALL PRIVILEGES ON DATABASE netdata_metrics TO netdata;
SQL
```

#### RHEL / Rocky / AlmaLinux

```bash
# Install PostgreSQL 16 from official repo
sudo dnf install -y https://download.postgresql.org/pub/repos/yum/reporpms/EL-$(rpm -E %rhel)-x86_64/pgdg-redhat-repo-latest.noarch.rpm
sudo dnf install -y postgresql16-server postgresql16

# Initialize and start
sudo /usr/pgsql-16/bin/postgresql-16-setup initdb
sudo systemctl enable --now postgresql-16

# Create database and user
sudo -u postgres /usr/pgsql-16/bin/psql <<'SQL'
CREATE USER netdata WITH PASSWORD 'CHANGE_ME_STRONG_PASSWORD';
CREATE DATABASE netdata_metrics OWNER netdata;
GRANT ALL PRIVILEGES ON DATABASE netdata_metrics TO netdata;
SQL
```

### Configure PostgreSQL to Accept Remote Connections

Edit `postgresql.conf`:

```bash
# Ubuntu: /etc/postgresql/16/main/postgresql.conf
# RHEL:   /var/lib/pgsql/16/data/postgresql.conf

listen_addresses = '*'          # Listen on all interfaces
max_connections = 200           # Increase for many nodes
shared_buffers = 512MB          # Adjust based on available RAM
work_mem = 16MB
```

Edit `pg_hba.conf` to allow connections from your node network:

```bash
# Ubuntu: /etc/postgresql/16/main/pg_hba.conf
# RHEL:   /var/lib/pgsql/16/data/pg_hba.conf

# Add these lines (adjust the CIDR to your network):
# TYPE  DATABASE        USER     ADDRESS              METHOD
host    netdata_metrics netdata  10.0.0.0/8           scram-sha-256
host    netdata_metrics netdata  172.16.0.0/12        scram-sha-256
host    netdata_metrics netdata  192.168.0.0/16       scram-sha-256
```

Restart PostgreSQL:

```bash
# Ubuntu
sudo systemctl restart postgresql

# RHEL
sudo systemctl restart postgresql-16
```

### Firewall Rules (Central Server)

```bash
# Ubuntu (ufw)
sudo ufw allow 5432/tcp comment "PostgreSQL for netdata-mcp"
sudo ufw allow 8765/tcp comment "MCP SSE server"

# RHEL (firewalld)
sudo firewall-cmd --permanent --add-port=5432/tcp
sudo firewall-cmd --permanent --add-port=8765/tcp
sudo firewall-cmd --reload
```

### Run Database Migrations on the Central Server

Build the sidecar binary on the central server (or copy a pre-built binary):

```bash
# Install Go 1.22+ if not present
# Ubuntu: sudo snap install go --classic
# RHEL:   sudo dnf install -y golang

git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp
go build -o netdata-postgres-mcp ./cmd/netdata-postgres-mcp

# Run migrations
export POSTGRES_DSN="postgres://netdata:CHANGE_ME_STRONG_PASSWORD@localhost:5432/netdata_metrics?sslmode=disable"
./netdata-postgres-mcp migrate
```

### Option B: Docker Compose (Quick Setup)

If you prefer Docker on the central server:

```bash
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp

cat > .env <<'EOF'
POSTGRES_DSN=postgres://netdata:netdata@postgres:5432/netdata_metrics?sslmode=disable
NETDATA_BASE_URL=http://localhost:19999
MCP_BIND_ADDR=0.0.0.0:8765
LOG_LEVEL=info
EOF

docker compose up -d postgres
# Wait for healthy, then run the MCP server container
docker compose up -d
```

> **Note:** When using Docker, the PostgreSQL port `5432` must be published (`-p 5432:5432`) so remote nodes can connect.

---

## Phase 2: Deploy Nodes

Each node needs:
1. Netdata Agent installed and running
2. `netdata-postgres-mcp` binary built or copied
3. Configuration pointing to the central PostgreSQL

Replace `CENTRAL_PG_HOST` below with your central PostgreSQL server's IP or hostname.

---

### Node Deployment: Ubuntu / Debian

#### Step 1: Install Netdata Agent

```bash
# One-line official installer
curl https://get.netdata.cloud/kickstart.sh > /tmp/netdata-kickstart.sh
sh /tmp/netdata-kickstart.sh --stable-channel

# Verify
sudo systemctl status netdata
curl -s http://localhost:19999/api/v1/info | head -20
```

#### Step 2: Build the Sidecar

```bash
# Install Go (if not present)
sudo snap install go --classic
# or: sudo apt install -y golang-go

# Clone and build
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git /opt/netdata-postgres-mcp
cd /opt/netdata-postgres-mcp
go build -o /usr/local/bin/netdata-postgres-mcp ./cmd/netdata-postgres-mcp
```

#### Step 3: Configure

```bash
sudo mkdir -p /etc/netdata-postgres-mcp

sudo tee /etc/netdata-postgres-mcp/config.yaml > /dev/null <<'EOF'
# Central PostgreSQL — all nodes write here
postgres_dsn: "postgres://netdata:CHANGE_ME_STRONG_PASSWORD@CENTRAL_PG_HOST:5432/netdata_metrics?sslmode=disable"

# Local Netdata agent
netdata_base_url: "http://localhost:19999"

# Unique identifier for this node
node_id: "ubuntu-web-01"

# Collection every 60 seconds
collection_interval_seconds: 60

# Which metrics to collect
enabled_contexts:
  - system.cpu
  - system.ram
  - system.swap
  - system.io
  - system.pgpgio
  - system.ip
  - disk.io
  - disk.ops
  - disk.util
  - disk.space
  - disk.inodes
  - apps.cpu
  - apps.mem

log_level: info
EOF
```

> **Important:** Change `node_id` to a unique name for each node (e.g., `ubuntu-web-01`, `ubuntu-db-02`). Change `CENTRAL_PG_HOST` to the actual IP/hostname.

#### Step 4: Create systemd Service

```bash
sudo tee /etc/systemd/system/netdata-postgres-mcp.service > /dev/null <<'EOF'
[Unit]
Description=Netdata PostgreSQL MCP Sidecar
Documentation=https://github.com/anhtuan-mai/netdata-postgres-mcp
After=network-online.target netdata.service
Wants=network-online.target

[Service]
Type=simple
User=netdata
Group=netdata
Environment=CONFIG_FILE=/etc/netdata-postgres-mcp/config.yaml
ExecStart=/usr/local/bin/netdata-postgres-mcp run
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadOnlyPaths=/etc/netdata-postgres-mcp

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now netdata-postgres-mcp

# Verify
sudo systemctl status netdata-postgres-mcp
sudo journalctl -u netdata-postgres-mcp -f
```

---

### Node Deployment: Red Hat Enterprise Linux (RHEL) / Rocky / AlmaLinux

#### Step 1: Install Netdata Agent

```bash
# One-line official installer
curl https://get.netdata.cloud/kickstart.sh > /tmp/netdata-kickstart.sh
sh /tmp/netdata-kickstart.sh --stable-channel

# Verify
sudo systemctl status netdata
curl -s http://localhost:19999/api/v1/info | head -20
```

#### Step 2: Build the Sidecar

```bash
# Install Go
sudo dnf install -y golang

# Clone and build
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git /opt/netdata-postgres-mcp
cd /opt/netdata-postgres-mcp
go build -o /usr/local/bin/netdata-postgres-mcp ./cmd/netdata-postgres-mcp

# SELinux: allow the binary to make network connections
sudo setsebool -P httpd_can_network_connect 1
# If SELinux blocks execution, set the proper context:
sudo restorecon -v /usr/local/bin/netdata-postgres-mcp
```

#### Step 3: Configure

```bash
sudo mkdir -p /etc/netdata-postgres-mcp

sudo tee /etc/netdata-postgres-mcp/config.yaml > /dev/null <<'EOF'
postgres_dsn: "postgres://netdata:CHANGE_ME_STRONG_PASSWORD@CENTRAL_PG_HOST:5432/netdata_metrics?sslmode=disable"
netdata_base_url: "http://localhost:19999"
node_id: "rhel-app-01"
collection_interval_seconds: 60

enabled_contexts:
  - system.cpu
  - system.ram
  - system.swap
  - system.io
  - system.pgpgio
  - system.ip
  - disk.io
  - disk.ops
  - disk.util
  - disk.space
  - disk.inodes
  - apps.cpu
  - apps.mem

log_level: info
EOF
```

#### Step 4: Create systemd Service

```bash
sudo tee /etc/systemd/system/netdata-postgres-mcp.service > /dev/null <<'EOF'
[Unit]
Description=Netdata PostgreSQL MCP Sidecar
Documentation=https://github.com/anhtuan-mai/netdata-postgres-mcp
After=network-online.target netdata.service
Wants=network-online.target

[Service]
Type=simple
User=netdata
Group=netdata
Environment=CONFIG_FILE=/etc/netdata-postgres-mcp/config.yaml
ExecStart=/usr/local/bin/netdata-postgres-mcp run
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
ReadOnlyPaths=/etc/netdata-postgres-mcp

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now netdata-postgres-mcp

# Verify
sudo systemctl status netdata-postgres-mcp
sudo journalctl -u netdata-postgres-mcp -f
```

#### Step 5: Firewall (RHEL)

The sidecar only makes **outbound** connections (to PostgreSQL:5432 and local Netdata:19999). No inbound ports need to be opened on the node unless you also want to expose the MCP server.

```bash
# Only if you want remote MCP access from this node:
sudo firewall-cmd --permanent --add-port=8765/tcp
sudo firewall-cmd --reload
```

---

### Node Deployment: Windows Server

#### Step 1: Install Netdata Agent

Netdata runs natively on Windows Server 2019+ (preview) or via WSL2:

**Option A: Native Windows Agent (Netdata v2+)**

```powershell
# Download the latest Windows MSI from https://github.com/netdata/netdata/releases
# Or use winget:
winget install Netdata.Netdata

# Verify
Invoke-RestMethod http://localhost:19999/api/v1/info | ConvertTo-Json
```

**Option B: WSL2 (Fallback)**

```powershell
# Enable WSL2 and install Ubuntu
wsl --install -d Ubuntu

# Inside WSL:
wsl -d Ubuntu -- bash -c "curl https://get.netdata.cloud/kickstart.sh > /tmp/netdata-kickstart.sh && sh /tmp/netdata-kickstart.sh --stable-channel"
```

#### Step 2: Build the Sidecar

```powershell
# Install Go (if not present)
winget install GoLang.Go
# Restart your terminal so Go is on PATH, or use the full path:
# & "C:\Program Files\Go\bin\go.exe"

# Clone and build
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git C:\netdata-postgres-mcp
cd C:\netdata-postgres-mcp
go build -o C:\netdata-postgres-mcp\netdata-postgres-mcp.exe .\cmd\netdata-postgres-mcp
```

#### Step 3: Configure

Create `C:\netdata-postgres-mcp\config.yaml`:

```yaml
postgres_dsn: "postgres://netdata:CHANGE_ME_STRONG_PASSWORD@CENTRAL_PG_HOST:5432/netdata_metrics?sslmode=disable"
netdata_base_url: "http://localhost:19999"
node_id: "win-iis-01"
collection_interval_seconds: 60

enabled_contexts:
  - system.cpu
  - system.ram
  - system.swap
  - system.io
  - system.pgpgio
  - system.ip
  - disk.io
  - disk.ops
  - disk.util
  - disk.space
  - disk.inodes
  - apps.cpu
  - apps.mem

log_level: info
```

#### Step 4: Register as a Windows Service

Use [NSSM (Non-Sucking Service Manager)](https://nssm.cc/) to run the sidecar as a Windows service:

```powershell
# Download NSSM
Invoke-WebRequest -Uri "https://nssm.cc/release/nssm-2.24.zip" -OutFile "$env:TEMP\nssm.zip"
Expand-Archive "$env:TEMP\nssm.zip" -DestinationPath "C:\nssm"

# Install the service
C:\nssm\nssm-2.24\win64\nssm.exe install NetdataPostgresMCP "C:\netdata-postgres-mcp\netdata-postgres-mcp.exe" "run"

# Set environment variables
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppEnvironmentExtra "CONFIG_FILE=C:\netdata-postgres-mcp\config.yaml"

# Set auto-restart
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppRestartDelay 10000

# Configure logging
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppStdout "C:\netdata-postgres-mcp\logs\stdout.log"
C:\nssm\nssm-2.24\win64\nssm.exe set NetdataPostgresMCP AppStderr "C:\netdata-postgres-mcp\logs\stderr.log"
New-Item -ItemType Directory -Force -Path "C:\netdata-postgres-mcp\logs"

# Start the service
Start-Service NetdataPostgresMCP

# Verify
Get-Service NetdataPostgresMCP
Get-Content "C:\netdata-postgres-mcp\logs\stderr.log" -Tail 20
```

**Alternative: sc.exe (no NSSM dependency)**

```powershell
# Create a wrapper batch script
Set-Content -Path "C:\netdata-postgres-mcp\run-service.bat" -Value @"
@echo off
set CONFIG_FILE=C:\netdata-postgres-mcp\config.yaml
C:\netdata-postgres-mcp\netdata-postgres-mcp.exe run
"@

# Register with sc.exe (requires a service wrapper like WinSW or similar)
# NSSM is recommended for non-service-aware executables
```

#### Step 5: Windows Firewall

```powershell
# Allow outbound to PostgreSQL (usually allowed by default)
# Only needed if outbound filtering is enabled:
New-NetFirewallRule -DisplayName "Netdata MCP to PostgreSQL" `
    -Direction Outbound -Protocol TCP -RemotePort 5432 `
    -Action Allow

# Only if exposing MCP server on this node:
New-NetFirewallRule -DisplayName "Netdata MCP SSE Server" `
    -Direction Inbound -Protocol TCP -LocalPort 8765 `
    -Action Allow
```

---

## Phase 3: Central MCP Server for AI Assistants

On the central PostgreSQL server (or any machine that can reach the database), run the MCP server so AI assistants can query all nodes' data:

### systemd Service (Linux)

```bash
sudo tee /etc/netdata-postgres-mcp/mcp-server.yaml > /dev/null <<'EOF'
postgres_dsn: "postgres://netdata:CHANGE_ME_STRONG_PASSWORD@localhost:5432/netdata_metrics?sslmode=disable"
mcp_bind_addr: "0.0.0.0:8765"
log_level: info
EOF

sudo tee /etc/systemd/system/netdata-mcp-server.service > /dev/null <<'EOF'
[Unit]
Description=Netdata MCP Server (centralized)
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=netdata
Group=netdata
Environment=CONFIG_FILE=/etc/netdata-postgres-mcp/mcp-server.yaml
ExecStart=/usr/local/bin/netdata-postgres-mcp run
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now netdata-mcp-server
```

> **Note:** On the central server you typically don't need a local Netdata agent unless you also want to monitor the central server itself. The `run` command starts both the collector and MCP server, but the collector will gracefully log warnings if no local Netdata is found. You can set `NETDATA_BASE_URL` to a dummy value if you only want the MCP server.

### Connect AI Assistants

#### Claude Desktop / Claude Code

Add to your MCP configuration:

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "url": "http://CENTRAL_PG_HOST:8765/sse"
    }
  }
}
```

Or for stdio transport (local only):

```json
{
  "mcpServers": {
    "netdata-metrics": {
      "command": "/usr/local/bin/netdata-postgres-mcp",
      "args": ["mcp"],
      "env": {
        "POSTGRES_DSN": "postgres://netdata:CHANGE_ME_STRONG_PASSWORD@CENTRAL_PG_HOST:5432/netdata_metrics?sslmode=disable"
      }
    }
  }
}
```

#### Cursor

In Cursor Settings → MCP Servers, add:

```
SSE URL: http://CENTRAL_PG_HOST:8765/sse
```

---

## Phase 4: Verification

### 1. Verify Nodes are Sending Data

On the central PostgreSQL server:

```sql
-- Connect to the database
psql -U netdata -d netdata_metrics -h localhost

-- List all registered nodes
SELECT node_id, hostname, netdata_base_url, updated_at
FROM netdata_nodes
ORDER BY updated_at DESC;

-- Check recent sample counts per node
SELECT node_id, COUNT(*) as samples, 
       MIN(collected_at) as oldest, 
       MAX(collected_at) as newest
FROM hardware_metric_samples
WHERE collected_at > NOW() - INTERVAL '1 hour'
GROUP BY node_id
ORDER BY node_id;

-- Verify latest metrics view
SELECT node_id, context, dimension, value, collected_at
FROM hardware_latest_metrics
ORDER BY node_id, context, dimension
LIMIT 20;
```

### 2. Verify via MCP (AI Assistant)

Ask your AI assistant:

- *"List all monitored nodes"*
- *"Show latest CPU and RAM for node ubuntu-web-01"*
- *"Compare CPU usage between ubuntu-web-01 and rhel-app-01 in the last hour"*
- *"Find hardware bottlenecks on win-iis-01 in the last 15 minutes"*
- *"Summarize hardware performance across all nodes"*

### 3. Check Service Logs

```bash
# Ubuntu / RHEL
sudo journalctl -u netdata-postgres-mcp --since "10 minutes ago"

# Windows
Get-Content "C:\netdata-postgres-mcp\logs\stderr.log" -Tail 50
```

---

## Node Reference Table

Use this table to plan your deployment. Each row is one node:

| Node ID | OS | IP | Netdata URL | Notes |
|---|---|---|---|---|
| `ubuntu-web-01` | Ubuntu 22.04 | 10.0.1.10 | http://localhost:19999 | Web server |
| `ubuntu-web-02` | Ubuntu 22.04 | 10.0.1.11 | http://localhost:19999 | Web server |
| `rhel-app-01` | RHEL 9 | 10.0.2.10 | http://localhost:19999 | App server |
| `rhel-db-01` | RHEL 9 | 10.0.2.11 | http://localhost:19999 | DB server |
| `win-iis-01` | Win Server 2022 | 10.0.3.10 | http://localhost:19999 | IIS server |
| `central-pg` | Ubuntu 22.04 | 10.0.0.5 | — | PostgreSQL + MCP |

---

## Production Recommendations

### PostgreSQL Tuning (for 10+ nodes)

```sql
-- Add retention policy: delete samples older than 30 days
DELETE FROM hardware_metric_samples
WHERE collected_at < NOW() - INTERVAL '30 days';

-- Schedule this as a cron job or pg_cron:
-- SELECT cron.schedule('metric-retention', '0 3 * * *',
--   $$DELETE FROM hardware_metric_samples WHERE collected_at < NOW() - INTERVAL '30 days'$$);
```

If using TimescaleDB:

```sql
-- Automatic chunk retention (much faster than DELETE)
SELECT add_retention_policy('hardware_metric_samples', INTERVAL '30 days');

-- Enable compression for older data
ALTER TABLE hardware_metric_samples SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'node_id,context,dimension'
);
SELECT add_compression_policy('hardware_metric_samples', INTERVAL '7 days');
```

### SSL/TLS for PostgreSQL

For production, enable SSL:

```bash
# In postgresql.conf:
ssl = on
ssl_cert_file = '/etc/ssl/certs/pg-server.crt'
ssl_key_file = '/etc/ssl/private/pg-server.key'
```

Update each node's DSN:

```yaml
postgres_dsn: "postgres://netdata:PASSWORD@CENTRAL_PG_HOST:5432/netdata_metrics?sslmode=require"
```

### Monitoring the Monitors

Add a cron job on the central server to alert if a node stops reporting:

```bash
# /etc/cron.d/netdata-mcp-health
*/5 * * * * netdata psql -U netdata -d netdata_metrics -tAc \
  "SELECT node_id FROM netdata_nodes WHERE updated_at < NOW() - INTERVAL '5 minutes'" \
  | while read node; do echo "ALERT: Node $node stopped reporting" | logger -t netdata-mcp; done
```

### Cross-compile Binaries

To avoid installing Go on every node, cross-compile from one build machine:

```bash
# Linux AMD64
GOOS=linux GOARCH=amd64 go build -o dist/netdata-postgres-mcp-linux-amd64 ./cmd/netdata-postgres-mcp

# Linux ARM64
GOOS=linux GOARCH=arm64 go build -o dist/netdata-postgres-mcp-linux-arm64 ./cmd/netdata-postgres-mcp

# Windows AMD64
GOOS=windows GOARCH=amd64 go build -o dist/netdata-postgres-mcp-windows-amd64.exe ./cmd/netdata-postgres-mcp
```

Then distribute the pre-built binary to each node — no Go toolchain needed.

---

## Quick-Add Checklist (New Node)

When adding a new node to the fleet:

- [ ] Install Netdata Agent and confirm http://localhost:19999 works
- [ ] Copy `netdata-postgres-mcp` binary to the node (or build from source)
- [ ] Create `config.yaml` with a **unique `node_id`** and the central `postgres_dsn`
- [ ] Register and start the service (systemd or NSSM)
- [ ] Verify the node appears in `SELECT * FROM netdata_nodes` on the central DB
- [ ] Confirm metrics are flowing: check `hardware_metric_samples` for the new `node_id`
- [ ] Test from the AI assistant: *"Show latest metrics for [new-node-id]"*

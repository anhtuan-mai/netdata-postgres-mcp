# Contributing to netdata-postgres-mcp

Thank you for your interest in contributing! This document provides guidelines for contributing to the netdata-postgres-mcp sidecar service.

## Getting started

### Prerequisites

- Go 1.23 or later
- PostgreSQL 14+
- A running Netdata Agent (for integration testing)
- Docker & Docker Compose (optional, for containerized development)

### Setting up a development environment

```bash
# Clone the repository
git clone https://github.com/anhtuan-mai/netdata-postgres-mcp.git
cd netdata-postgres-mcp

# Install dependencies
go mod download

# Set up a local PostgreSQL database
export POSTGRES_DSN="postgres://netdata:netdata@localhost:5432/netdata_metrics?sslmode=disable"

# Run migrations
go run ./cmd/netdata-postgres-mcp migrate

# Verify everything works
make test
make lint
```

### Using Docker Compose for development

```bash
cp .env.example .env
# Edit .env with your local Netdata URL
docker compose up -d
```

## Project structure

```
cmd/netdata-postgres-mcp/   # Main entry point and CLI commands
internal/
  collector/                 # Netdata API client and metric collection
  config/                    # Configuration loading (YAML + env vars)
  mcp/                       # MCP server and tool handlers
  metrics/                   # Prometheus-compatible self-observability
  scheduler/                 # Periodic collection and retention cleanup
  store/                     # PostgreSQL storage and migrations
    migrations/              # SQL migration files
grafana/                     # Grafana dashboard templates
```

## Development workflow

### 1. Create a branch

```bash
git checkout -b feature/your-feature-name
```

### 2. Make your changes

Follow the existing code patterns:

- **Error handling**: Wrap errors with `fmt.Errorf("context: %w", err)` for traceability
- **Logging**: Use `slog` structured logging (`logger.Info("message", "key", value)`)
- **Imports**: Group standard library, external, and internal imports separated by blank lines
- **Comments**: Add `// FunctionName does...` doc comments on all exported functions
- **License**: Include `// SPDX-License-Identifier: GPL-3.0-or-later` at the top of every Go file

### 3. Write tests

- Place tests in `*_test.go` files alongside the code they test
- Use table-driven tests where appropriate
- Tests that require PostgreSQL should be gated with a build tag or environment check:

```go
func TestWithDB(t *testing.T) {
    dsn := os.Getenv("POSTGRES_DSN")
    if dsn == "" {
        t.Skip("POSTGRES_DSN not set, skipping integration test")
    }
    // ...
}
```

### 4. Run checks

```bash
make test       # Run all tests
make lint       # Run go vet + staticcheck
make build      # Ensure it compiles
```

### 5. Commit and push

Write clear, concise commit messages:

```
Add retry logic to metric collection

Collection now retries up to 3 times with exponential backoff
when the Netdata API returns an error.
```

### 6. Open a pull request

- Provide a clear description of what changed and why
- Reference any related issues
- Ensure CI checks pass

## Code style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Keep functions focused and under ~60 lines where practical
- Use meaningful variable names; avoid single-letter names outside short loops
- Prefer returning errors over panicking
- Use `context.Context` for cancellation and timeouts

## Adding new MCP tools

To add a new MCP tool:

1. Define the tool in `internal/mcp/server.go` using `mcp.NewTool()`
2. Add the handler method on the `Server` struct
3. Use `withTimeout(ctx)` for a 30-second deadline
4. Register the tool in the `New()` constructor
5. Add tests and update README.md

## Adding new metrics

To add a new Prometheus metric:

1. Add the counter/gauge field to `internal/metrics/metrics.go` (`Metrics` struct)
2. Record the metric where the event occurs (e.g., in the scheduler or collector)
3. Add the exposition line in `Handler()`
4. Update the Grafana dashboard in `grafana/dashboard.json`

## Database migrations

- Place new migration files in `internal/store/migrations/`
- Use sequential numbering: `003_description.sql`, `004_description.sql`
- Migrations must be idempotent (`IF NOT EXISTS`, `ON CONFLICT DO NOTHING`)
- Test migrations against a clean database and against an existing schema

## Reporting bugs

Open an issue with:

- Steps to reproduce
- Expected vs. actual behavior
- Go version, OS, PostgreSQL version
- Relevant log output (set `LOG_LEVEL=debug` for verbose logs)

## License

By contributing, you agree that your contributions will be licensed under GPL-3.0-or-later, the same license as the project.

#!/usr/bin/env bash
# scripts/e2e-test.sh — Run the end-to-end test suite via Docker Compose.
#
# Spins up PostgreSQL + Netdata + sidecar, runs the test runner container,
# then tears everything down regardless of outcome.
#
# Usage: ./scripts/e2e-test.sh
# Exit code: 0 on success, 1 on failure.

set -euo pipefail

COMPOSE_FILE="docker-compose.e2e.yml"
PROJECT="netdata-mcp-e2e"

cleanup() {
    echo ""
    echo "--- Tearing down e2e stack ---"
    docker compose -f "$COMPOSE_FILE" -p "$PROJECT" down -v --remove-orphans 2>/dev/null || true
}
trap cleanup EXIT

echo "=== Building and starting e2e stack ==="
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" build --quiet
docker compose -f "$COMPOSE_FILE" -p "$PROJECT" up --abort-on-container-exit --exit-code-from e2e-runner

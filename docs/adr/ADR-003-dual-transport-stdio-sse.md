# ADR-003: Dual Transport — stdio and HTTP/SSE

## Status

Accepted

## Date

2026-06-14

## Context

The Model Context Protocol (MCP) defines a standard for AI assistants to interact with external tools and data sources. Different AI clients consume MCP servers through different transport mechanisms:

- **Claude Desktop** communicates with MCP servers via **stdio** — it launches the server as a subprocess and exchanges JSON-RPC messages over stdin/stdout.
- **Cursor** and other web-based or remote AI clients communicate with MCP servers via **HTTP with Server-Sent Events (SSE)** — the client connects to an HTTP endpoint, sends requests via POST, and receives streaming responses over an SSE channel.

The sidecar needs to serve both types of clients to maximize compatibility across the AI tooling ecosystem. Forcing users to choose a single transport at build time or requiring separate binaries would complicate deployment and documentation.

## Decision

We support both stdio and HTTP/SSE transports in a single binary, selectable via a command-line flag (`--transport stdio|sse`) or environment variable. The implementation uses the `mcp-go` library, which provides built-in support for both transports:

1. **stdio mode** (`--transport stdio`, default): The sidecar reads JSON-RPC requests from stdin and writes responses to stdout. Intended for direct integration with Claude Desktop via its `mcpServers` configuration.
2. **SSE mode** (`--transport sse`): The sidecar starts an HTTP server (default port 8080, configurable via `--port`) that exposes an SSE endpoint for MCP communication. Intended for remote clients like Cursor or custom web-based MCP consumers.

Both transports share the same tool registration, handler logic, and database connection layer. The transport is purely a communication concern and does not affect tool behavior.

## Alternatives Considered

- **stdio only**: Simpler implementation, but excludes remote/web clients entirely. Rejected because Cursor is a major MCP client that requires HTTP/SSE.
- **SSE only**: Works for remote clients, but Claude Desktop's native integration expects stdio. Users would need a proxy (e.g., `mcp-remote`) to bridge HTTP/SSE to stdio. Rejected because it adds deployment complexity for the most common use case.
- **Separate binaries**: Build two binaries, one for each transport. Rejected because it doubles the build/release/documentation burden for what is ultimately a single configuration flag.
- **WebSocket transport**: Use WebSockets instead of SSE for the HTTP transport. Rejected because the MCP specification and `mcp-go` library standardize on SSE, and deviating from the spec would break client compatibility.

## Consequences

**Positive:**

- A single binary serves all major AI clients (Claude Desktop, Cursor, and others).
- Users select the transport with a simple flag — no need for separate builds or adapter proxies.
- The `mcp-go` library handles the transport-layer complexity, keeping our code focused on tool logic.
- Easy to add new transports in the future if the MCP specification evolves.

**Negative:**

- The SSE transport requires the sidecar to run as a long-lived HTTP server, which has different operational characteristics (port management, TLS, reverse proxying) compared to the ephemeral stdio mode.
- Testing must cover both transport paths, though the shared handler layer minimizes divergence.
- The `mcp-go` library becomes a critical dependency. If it falls behind the MCP specification, we may need to fork or replace it.

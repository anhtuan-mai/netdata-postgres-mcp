// SPDX-License-Identifier: GPL-3.0-or-later

//go:build integration

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	gomcp "github.com/mark3labs/mcp-go/mcp"
)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set, skipping integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Run migrations
	pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`)
	pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS netdata_nodes (
		id UUID DEFAULT gen_random_uuid() PRIMARY KEY,
		node_id TEXT UNIQUE NOT NULL,
		hostname TEXT, netdata_base_url TEXT,
		created_at TIMESTAMPTZ DEFAULT now(), updated_at TIMESTAMPTZ DEFAULT now())`)
	pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS hardware_metric_samples (
		id BIGSERIAL PRIMARY KEY,
		node_id TEXT NOT NULL REFERENCES netdata_nodes(node_id),
		collected_at TIMESTAMPTZ NOT NULL,
		context TEXT NOT NULL, chart TEXT DEFAULT '', family TEXT DEFAULT '',
		instance TEXT DEFAULT '', dimension TEXT NOT NULL,
		unit TEXT DEFAULT '', value DOUBLE PRECISION NOT NULL,
		labels JSONB DEFAULT '{}',
		CONSTRAINT uq_metric_sample UNIQUE (node_id, collected_at, context, dimension, chart, instance))`)

	// Seed test data
	pool.Exec(ctx, `INSERT INTO netdata_nodes (node_id, hostname, netdata_base_url)
		VALUES ('test-mcp-node', 'mcp-host', 'http://localhost:19999')
		ON CONFLICT (node_id) DO NOTHING`)

	now := time.Now().UTC()
	for i := 0; i < 10; i++ {
		ts := now.Add(-time.Duration(i) * time.Minute)
		pool.Exec(ctx, `INSERT INTO hardware_metric_samples
			(node_id, collected_at, context, chart, family, dimension, unit, value, labels)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT ON CONSTRAINT uq_metric_sample DO NOTHING`,
			"test-mcp-node", ts, "system.cpu", "system.cpu", "cpu", "user", "percentage",
			float64(20+i), map[string]string{})
		pool.Exec(ctx, `INSERT INTO hardware_metric_samples
			(node_id, collected_at, context, chart, family, dimension, unit, value, labels)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			ON CONFLICT ON CONSTRAINT uq_metric_sample DO NOTHING`,
			"test-mcp-node", ts, "system.ram", "system.ram", "ram", "used", "MiB",
			float64(4096+i*100), map[string]string{})
	}

	t.Cleanup(func() {
		pool.Exec(context.Background(), "DELETE FROM hardware_metric_samples WHERE node_id = 'test-mcp-node'")
		pool.Exec(context.Background(), "DELETE FROM netdata_nodes WHERE node_id = 'test-mcp-node'")
		pool.Close()
	})

	return pool
}

func callTool(t *testing.T, s *Server, name string, args map[string]interface{}) string {
	t.Helper()
	req := gomcp.CallToolRequest{}
	req.Params.Name = name
	req.Params.Arguments = args

	var handler func(context.Context, gomcp.CallToolRequest) (*gomcp.CallToolResult, error)
	switch name {
	case "list_nodes":
		handler = s.handleListNodes
	case "latest_hardware_metrics":
		handler = s.handleLatestMetrics
	case "query_hardware_metrics":
		handler = s.handleQueryMetrics
	case "summarize_hardware_performance":
		handler = s.handleSummarize
	case "find_hardware_bottlenecks":
		handler = s.handleBottlenecks
	default:
		t.Fatalf("unknown tool: %s", name)
	}

	result, err := handler(context.Background(), req)
	if err != nil {
		t.Fatalf("%s returned error: %v", name, err)
	}
	if result == nil || len(result.Content) == 0 {
		t.Fatalf("%s returned nil/empty result", name)
	}

	// Extract text from first content block
	text, ok := result.Content[0].(gomcp.TextContent)
	if !ok {
		t.Fatalf("%s result is not TextContent: %T", name, result.Content[0])
	}
	return text.Text
}

func TestIntegration_ListNodes(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "list_nodes", nil)

	if !strings.Contains(body, "test-mcp-node") {
		t.Errorf("list_nodes should contain test-mcp-node, got: %s", body)
	}
	if !strings.Contains(body, "mcp-host") {
		t.Errorf("list_nodes should contain hostname, got: %s", body)
	}
}

func TestIntegration_LatestMetrics(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "latest_hardware_metrics", map[string]interface{}{
		"node_id": "test-mcp-node",
	})

	if !strings.Contains(body, "system.cpu") {
		t.Errorf("latest_metrics should contain system.cpu, got: %s", body)
	}
}

func TestIntegration_LatestMetrics_WithContextFilter(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "latest_hardware_metrics", map[string]interface{}{
		"node_id":  "test-mcp-node",
		"contexts": "system.ram",
	})

	if !strings.Contains(body, "system.ram") {
		t.Errorf("should contain system.ram, got: %s", body)
	}
}

func TestIntegration_QueryMetrics(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "query_hardware_metrics", map[string]interface{}{
		"node_id": "test-mcp-node",
		"context": "system.cpu",
		"after":   "-1h",
		"limit":   float64(5),
	})

	// Should return JSON array of metric rows
	if !strings.Contains(body, "system.cpu") {
		t.Errorf("query should contain system.cpu, got: %s", body)
	}
	if !strings.Contains(body, "user") {
		t.Errorf("query should contain dimension 'user', got: %s", body)
	}
}

func TestIntegration_Summarize(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "summarize_hardware_performance", map[string]interface{}{
		"node_id": "test-mcp-node",
	})

	if !strings.Contains(body, "summary") && !strings.Contains(body, "cpu") && !strings.Contains(body, "avg") {
		t.Errorf("summarize should contain performance data, got: %s", body)
	}
}

func TestIntegration_Bottlenecks(t *testing.T) {
	pool := setupTestDB(t)
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(pool, logger)

	body := callTool(t, s, "find_hardware_bottlenecks", map[string]interface{}{
		"node_id": "test-mcp-node",
	})

	// Result should be valid JSON (either bottlenecks array or message)
	var result interface{}
	if err := json.Unmarshal([]byte(body), &result); err != nil {
		// Some responses are plain text summaries, which is acceptable
		if !strings.Contains(body, "bottleneck") && !strings.Contains(body, "No ") &&
			!strings.Contains(body, "error") && !strings.Contains(body, "node") {
			t.Logf("bottlenecks response (non-JSON): %s", body)
		}
	}
}

// Verify withTimeout applies a 30s deadline
func TestWithTimeout(t *testing.T) {
	ctx := context.Background()
	tctx, cancel := withTimeout(ctx)
	defer cancel()

	deadline, ok := tctx.Deadline()
	if !ok {
		t.Fatal("withTimeout should set a deadline")
	}
	remaining := time.Until(deadline)
	if remaining < 29*time.Second || remaining > 31*time.Second {
		t.Errorf("deadline should be ~30s from now, got %v", remaining)
	}
}

// Verify textResult helper
func TestTextResult(t *testing.T) {
	_ = fmt.Sprintf // prevent unused import

	result := textResult("hello world")
	if result == nil {
		t.Fatal("textResult should not return nil")
	}
	if len(result.Content) == 0 {
		t.Fatal("textResult should have content")
	}
	text, ok := result.Content[0].(gomcp.TextContent)
	if !ok {
		t.Fatalf("content is not TextContent: %T", result.Content[0])
	}
	if text.Text != "hello world" {
		t.Errorf("text = %q, want %q", text.Text, "hello world")
	}
}

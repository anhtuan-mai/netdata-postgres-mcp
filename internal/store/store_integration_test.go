// SPDX-License-Identifier: GPL-3.0-or-later

//go:build integration

package store

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

// These tests require a running PostgreSQL instance.
// Run with: go test -tags=integration -count=1 ./internal/store/
// Set POSTGRES_DSN to the test database connection string.

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN not set, skipping integration test")
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx := context.Background()

	st, err := New(ctx, dsn, logger)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	// Run migrations
	if err := st.Migrate(ctx); err != nil {
		st.Close()
		t.Fatalf("failed to migrate: %v", err)
	}

	t.Cleanup(func() {
		// Clean up test data
		st.pool.Exec(context.Background(), "DELETE FROM hardware_metric_samples WHERE node_id LIKE 'test-%'")
		st.pool.Exec(context.Background(), "DELETE FROM netdata_nodes WHERE node_id LIKE 'test-%'")
		st.Close()
	})

	return st
}

func TestIntegration_Migrate(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Migrate should be idempotent — running again should succeed
	if err := st.Migrate(ctx); err != nil {
		t.Errorf("second Migrate() failed: %v", err)
	}
}

func TestIntegration_UpsertNode(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Insert new node
	err := st.UpsertNode(ctx, "test-node-1", "test-host-1", "http://10.0.0.1:19999")
	if err != nil {
		t.Fatalf("UpsertNode insert failed: %v", err)
	}

	// Update same node (upsert)
	err = st.UpsertNode(ctx, "test-node-1", "test-host-1-updated", "http://10.0.0.2:19999")
	if err != nil {
		t.Fatalf("UpsertNode update failed: %v", err)
	}

	// Verify the update took effect
	nodes, err := st.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	var found bool
	for _, n := range nodes {
		if n.NodeID == "test-node-1" {
			found = true
			if n.Hostname != "test-host-1-updated" {
				t.Errorf("hostname = %q, want %q", n.Hostname, "test-host-1-updated")
			}
			if n.NetdataBaseURL != "http://10.0.0.2:19999" {
				t.Errorf("base_url = %q, want %q", n.NetdataBaseURL, "http://10.0.0.2:19999")
			}
		}
	}
	if !found {
		t.Error("test-node-1 not found in ListNodes results")
	}
}

func TestIntegration_InsertSamples(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Setup node first
	if err := st.UpsertNode(ctx, "test-node-2", "test-host", "http://localhost:19999"); err != nil {
		t.Fatalf("UpsertNode failed: %v", err)
	}

	now := time.Now().UTC().Truncate(time.Second)

	samples := []MetricSample{
		{
			NodeID:      "test-node-2",
			CollectedAt: now,
			Context:     "system.cpu",
			Chart:       "system.cpu",
			Family:      "cpu",
			Dimension:   "user",
			Unit:        "percentage",
			Value:       45.2,
			Labels:      map[string]string{"env": "test"},
		},
		{
			NodeID:      "test-node-2",
			CollectedAt: now,
			Context:     "system.cpu",
			Chart:       "system.cpu",
			Family:      "cpu",
			Dimension:   "system",
			Unit:        "percentage",
			Value:       12.8,
			Labels:      map[string]string{"env": "test"},
		},
		{
			NodeID:      "test-node-2",
			CollectedAt: now,
			Context:     "system.ram",
			Chart:       "system.ram",
			Family:      "ram",
			Dimension:   "used",
			Unit:        "MiB",
			Value:       4096,
		},
	}

	inserted, err := st.InsertSamples(ctx, samples)
	if err != nil {
		t.Fatalf("InsertSamples failed: %v", err)
	}
	if inserted != 3 {
		t.Errorf("inserted = %d, want 3", inserted)
	}

	// Insert same samples again — should be 0 due to upsert
	inserted2, err := st.InsertSamples(ctx, samples)
	if err != nil {
		t.Fatalf("InsertSamples (duplicate) failed: %v", err)
	}
	if inserted2 != 0 {
		t.Errorf("duplicate inserted = %d, want 0", inserted2)
	}
}

func TestIntegration_InsertSamples_Empty(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	inserted, err := st.InsertSamples(ctx, nil)
	if err != nil {
		t.Fatalf("InsertSamples(nil) failed: %v", err)
	}
	if inserted != 0 {
		t.Errorf("inserted = %d, want 0", inserted)
	}
}

func TestIntegration_ListNodes(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	// Insert two nodes
	if err := st.UpsertNode(ctx, "test-node-list-a", "host-a", "http://a:19999"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertNode(ctx, "test-node-list-b", "host-b", "http://b:19999"); err != nil {
		t.Fatal(err)
	}

	nodes, err := st.ListNodes(ctx)
	if err != nil {
		t.Fatalf("ListNodes failed: %v", err)
	}

	foundA, foundB := false, false
	for _, n := range nodes {
		if n.NodeID == "test-node-list-a" {
			foundA = true
		}
		if n.NodeID == "test-node-list-b" {
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Errorf("expected both test nodes, foundA=%v foundB=%v, nodes=%v", foundA, foundB, nodes)
	}
}

func TestIntegration_DeleteOldSamples(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	if err := st.UpsertNode(ctx, "test-node-retention", "host-ret", "http://ret:19999"); err != nil {
		t.Fatal(err)
	}

	// Insert a sample from 60 days ago
	oldTime := time.Now().UTC().Add(-60 * 24 * time.Hour)
	recentTime := time.Now().UTC()

	samples := []MetricSample{
		{
			NodeID:      "test-node-retention",
			CollectedAt: oldTime,
			Context:     "system.cpu",
			Chart:       "system.cpu",
			Dimension:   "user",
			Unit:        "percentage",
			Value:       50.0,
		},
		{
			NodeID:      "test-node-retention",
			CollectedAt: recentTime,
			Context:     "system.cpu",
			Chart:       "system.cpu",
			Dimension:   "system",
			Unit:        "percentage",
			Value:       10.0,
		},
	}

	_, err := st.InsertSamples(ctx, samples)
	if err != nil {
		t.Fatalf("InsertSamples failed: %v", err)
	}

	// Delete samples older than 30 days — should remove the old one
	deleted, err := st.DeleteOldSamples(ctx, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("DeleteOldSamples failed: %v", err)
	}
	if deleted < 1 {
		t.Errorf("deleted = %d, want >= 1", deleted)
	}
}

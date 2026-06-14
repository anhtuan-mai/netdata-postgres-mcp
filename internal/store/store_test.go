// SPDX-License-Identifier: GPL-3.0-or-later

package store

import (
	"testing"
	"time"
)

func TestMetricSample_Fields(t *testing.T) {
	now := time.Now()
	s := MetricSample{
		NodeID:      "node-1",
		CollectedAt: now,
		Context:     "system.cpu",
		Chart:       "system.cpu",
		Family:      "cpu",
		Instance:    "",
		Dimension:   "user",
		Unit:        "percentage",
		Value:       42.5,
		Labels:      map[string]string{"env": "prod"},
	}

	if s.NodeID != "node-1" {
		t.Errorf("NodeID = %q", s.NodeID)
	}
	if s.Context != "system.cpu" {
		t.Errorf("Context = %q", s.Context)
	}
	if s.Value != 42.5 {
		t.Errorf("Value = %f, want 42.5", s.Value)
	}
	if s.Labels["env"] != "prod" {
		t.Errorf("Labels[env] = %q, want prod", s.Labels["env"])
	}
	if !s.CollectedAt.Equal(now) {
		t.Errorf("CollectedAt = %v, want %v", s.CollectedAt, now)
	}
}

func TestMetricSample_NilLabels(t *testing.T) {
	s := MetricSample{
		NodeID:    "node-1",
		Context:   "system.ram",
		Dimension: "used",
		Value:     1024,
	}

	// Labels should be nil by default (handled during insert)
	if s.Labels != nil {
		t.Errorf("Labels should be nil, got %v", s.Labels)
	}
}

func TestNodeInfo_Fields(t *testing.T) {
	now := time.Now()
	n := NodeInfo{
		NodeID:          "node-1",
		Hostname:        "web-server-01",
		NetdataBaseURL:  "http://10.0.0.1:19999",
		LastCollectedAt: &now,
	}

	if n.NodeID != "node-1" {
		t.Errorf("NodeID = %q", n.NodeID)
	}
	if n.Hostname != "web-server-01" {
		t.Errorf("Hostname = %q", n.Hostname)
	}
	if n.NetdataBaseURL != "http://10.0.0.1:19999" {
		t.Errorf("NetdataBaseURL = %q", n.NetdataBaseURL)
	}
	if n.LastCollectedAt == nil || !n.LastCollectedAt.Equal(now) {
		t.Errorf("LastCollectedAt = %v, want %v", n.LastCollectedAt, now)
	}
}

func TestNodeInfo_NilLastCollected(t *testing.T) {
	n := NodeInfo{
		NodeID:   "node-2",
		Hostname: "db-server",
	}

	if n.LastCollectedAt != nil {
		t.Errorf("LastCollectedAt should be nil for new node, got %v", n.LastCollectedAt)
	}
}

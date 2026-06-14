// SPDX-License-Identifier: GPL-3.0-or-later

package webhook

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func TestNotifier_Send(t *testing.T) {
	var received Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier([]Config{{
		URL:           server.URL,
		MinConfidence: 0.5,
		IncludeEvidence: true,
	}}, logger)

	n.Notify(context.Background(), Payload{
		NodeID:         "test-node",
		BottleneckType: "cpu",
		Confidence:     0.85,
		Explanation:    "high CPU usage",
		Evidence: []EvidenceDetail{
			{Context: "system.cpu", Dimension: "user", AvgValue: 90, MaxValue: 99},
		},
	})

	if received.NodeID != "test-node" {
		t.Errorf("NodeID = %q, want test-node", received.NodeID)
	}
	if received.BottleneckType != "cpu" {
		t.Errorf("BottleneckType = %q, want cpu", received.BottleneckType)
	}
	if len(received.Evidence) != 1 {
		t.Errorf("Evidence length = %d, want 1", len(received.Evidence))
	}
}

func TestNotifier_BelowConfidence(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier([]Config{{
		URL:           server.URL,
		MinConfidence: 0.9,
	}}, logger)

	n.Notify(context.Background(), Payload{
		NodeID:         "test-node",
		BottleneckType: "cpu",
		Confidence:     0.5,
	})

	if called {
		t.Error("webhook should not be called when confidence is below threshold")
	}
}

func TestNotifier_Cooldown(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier([]Config{{
		URL:             server.URL,
		MinConfidence:   0.5,
		CooldownMinutes: 60, // 1 hour cooldown
	}}, logger)

	payload := Payload{
		NodeID:         "test-node",
		BottleneckType: "cpu",
		Confidence:     0.85,
	}

	n.Notify(context.Background(), payload) // should send
	n.Notify(context.Background(), payload) // should be suppressed by cooldown

	if callCount != 1 {
		t.Errorf("expected 1 call (second suppressed by cooldown), got %d", callCount)
	}
}

func TestNotifier_NoBottleneck(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier([]Config{{URL: server.URL}}, logger)

	n.Notify(context.Background(), Payload{
		NodeID:         "test-node",
		BottleneckType: "none",
		Confidence:     0.9,
	})

	if called {
		t.Error("webhook should not be called for bottleneck_type=none")
	}
}

func TestNotifier_ExcludeEvidence(t *testing.T) {
	var received Payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier([]Config{{
		URL:             server.URL,
		MinConfidence:   0.5,
		IncludeEvidence: false,
	}}, logger)

	n.Notify(context.Background(), Payload{
		NodeID:         "test-node",
		BottleneckType: "cpu",
		Confidence:     0.85,
		Evidence:       []EvidenceDetail{{Context: "system.cpu", Dimension: "user"}},
	})

	if received.Evidence != nil {
		t.Errorf("evidence should be nil when IncludeEvidence=false, got %v", received.Evidence)
	}
}

func TestNotifier_NoConfigs(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	n := NewNotifier(nil, logger)

	// Should not panic with nil configs
	n.Notify(context.Background(), Payload{
		NodeID:         "test-node",
		BottleneckType: "cpu",
		Confidence:     0.9,
	})
}

func TestPayload_JSON(t *testing.T) {
	p := Payload{
		Timestamp:      time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC),
		NodeID:         "node1",
		BottleneckType: "disk",
		Confidence:     0.75,
		Explanation:    "disk utilization high",
		Evidence: []EvidenceDetail{
			{Context: "disk.util", Dimension: "sda", MaxValue: 95.5},
		},
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var decoded Payload
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if decoded.NodeID != p.NodeID || decoded.BottleneckType != p.BottleneckType {
		t.Errorf("roundtrip mismatch")
	}
}

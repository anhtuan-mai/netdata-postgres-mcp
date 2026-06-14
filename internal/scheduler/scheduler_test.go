// SPDX-License-Identifier: GPL-3.0-or-later

package scheduler

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(nil, nil, 30, "node-1", "host-1", "http://localhost:19999", logger)

	if s.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s", s.interval)
	}
	if s.nodeID != "node-1" {
		t.Errorf("nodeID = %q, want %q", s.nodeID, "node-1")
	}
	if s.hostname != "host-1" {
		t.Errorf("hostname = %q, want %q", s.hostname, "host-1")
	}
	if s.baseURL != "http://localhost:19999" {
		t.Errorf("baseURL = %q", s.baseURL)
	}
	if s.retentionDays != 30 {
		t.Errorf("retentionDays = %d, want 30 (default)", s.retentionDays)
	}
}

func TestSetRetentionDays(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	s := New(nil, nil, 60, "n", "h", "http://localhost:19999", logger)

	s.SetRetentionDays(7)
	if s.retentionDays != 7 {
		t.Errorf("retentionDays = %d, want 7", s.retentionDays)
	}

	s.SetRetentionDays(0)
	if s.retentionDays != 0 {
		t.Errorf("retentionDays = %d, want 0", s.retentionDays)
	}
}

func TestNew_IntervalConversion(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	tests := []struct {
		input int
		want  time.Duration
	}{
		{1, 1 * time.Second},
		{60, 60 * time.Second},
		{300, 300 * time.Second},
	}

	for _, tt := range tests {
		s := New(nil, nil, tt.input, "n", "h", "http://localhost:19999", logger)
		if s.interval != tt.want {
			t.Errorf("New(%d).interval = %v, want %v", tt.input, s.interval, tt.want)
		}
	}
}

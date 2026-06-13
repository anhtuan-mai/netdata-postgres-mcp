// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"math"
	"testing"
	"time"
)

func TestParseTimeArg_Relative(t *testing.T) {
	tests := []struct {
		input    string
		wantErr  bool
		maxAge   time.Duration // result should be within this duration of now
	}{
		{"-1h", false, 62 * time.Minute},
		{"-30m", false, 32 * time.Minute},
		{"-15m", false, 17 * time.Minute},
		{"-24h", false, 25 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseTimeArg(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimeArg(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			age := time.Since(got)
			if age < 0 || age > tt.maxAge {
				t.Errorf("parseTimeArg(%q) = %v, age %v out of expected range", tt.input, got, age)
			}
		})
	}
}

func TestParseTimeArg_ISO(t *testing.T) {
	tests := []struct {
		input   string
		wantErr bool
	}{
		{"2025-01-15T10:30:00Z", false},
		{"2025-01-15T10:30:00+05:00", false},
		{"2025-01-15 10:30:00", false},
		{"2025-01-15", false},
		{"not-a-date", true},
		{"", false}, // empty returns now
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := parseTimeArg(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseTimeArg(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestBuildSummary(t *testing.T) {
	from := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2025, 1, 1, 1, 0, 0, 0, time.UTC)

	stats := map[string]map[string]*DimStatsPublic{
		"system.cpu": {
			"user":   {Sum: 150, Count: 10, Max: 25, Min: 5},
			"system": {Sum: 80, Count: 10, Max: 15, Min: 3},
			"idle":   {Sum: 700, Count: 10, Max: 85, Min: 60},
		},
		"system.ram": {
			"used":      {Sum: 40000, Count: 10, Max: 4500, Min: 3500},
			"free":      {Sum: 10000, Count: 10, Max: 1200, Min: 800},
			"cached":    {Sum: 15000, Count: 10, Max: 1600, Min: 1400},
			"available": {Sum: 25000, Count: 10, Max: 2800, Min: 2200},
		},
	}

	result := BuildSummary("test-node", from, to, stats)

	if result.NodeID != "test-node" {
		t.Errorf("nodeID = %q, want test-node", result.NodeID)
	}

	// CPU: user avg=15, system avg=8, total non-idle avg=23
	if result.CPU == nil {
		t.Fatal("expected CPU summary")
	}
	if result.CPU.AveragePercent != 23.0 {
		t.Errorf("CPU avg = %f, want 23.0", result.CPU.AveragePercent)
	}
	if result.CPU.PeakPercent != 25.0 {
		t.Errorf("CPU peak = %f, want 25.0", result.CPU.PeakPercent)
	}

	// RAM
	if result.RAM == nil {
		t.Fatal("expected RAM summary")
	}
	if result.RAM.AveragePercent != 4000.0 {
		t.Errorf("RAM avg = %f, want 4000.0", result.RAM.AveragePercent)
	}

	// Summary text should exist
	if result.Summary == "" {
		t.Error("expected non-empty summary text")
	}

	// Top abnormal should not include idle/free/cached/available
	for _, a := range result.TopAbnormal {
		if a.Dimension == "idle" || a.Dimension == "free" || a.Dimension == "available" || a.Dimension == "cached" {
			t.Errorf("top abnormal should not include %q", a.Dimension)
		}
	}
}

func TestDetectBottlenecks_CPU(t *testing.T) {
	aggs := []DimAgg{
		{"system.cpu", "user", nil, 60, 95, 10},
		{"system.cpu", "system", nil, 25, 40, 10},
		{"system.cpu", "idle", nil, 15, 40, 10},
	}

	result := DetectBottlenecks("test", aggs)
	if result.BottleneckType != "cpu" {
		t.Errorf("expected cpu bottleneck, got %s", result.BottleneckType)
	}
	if result.Confidence <= 0 {
		t.Errorf("expected positive confidence, got %f", result.Confidence)
	}
	if len(result.Evidence) == 0 {
		t.Error("expected evidence")
	}
}

func TestDetectBottlenecks_Disk(t *testing.T) {
	aggs := []DimAgg{
		{"system.cpu", "user", nil, 10, 20, 10},
		{"system.cpu", "system", nil, 5, 10, 10},
		{"system.cpu", "idle", nil, 85, 90, 10},
		{"system.cpu", "iowait", nil, 15, 30, 10},
		{"disk.util", "utilization", strPtr("sda"), 70, 98, 10},
	}

	result := DetectBottlenecks("test", aggs)
	if result.BottleneckType != "disk" {
		t.Errorf("expected disk bottleneck, got %s", result.BottleneckType)
	}
}

func TestDetectBottlenecks_None(t *testing.T) {
	aggs := []DimAgg{
		{"system.cpu", "user", nil, 5, 10, 10},
		{"system.cpu", "system", nil, 3, 5, 10},
		{"system.cpu", "idle", nil, 92, 95, 10},
		{"system.ram", "used", nil, 2000, 2500, 10},
		{"system.ram", "free", nil, 6000, 6500, 10},
	}

	result := DetectBottlenecks("test", aggs)
	if result.BottleneckType != "none" {
		t.Errorf("expected no bottleneck, got %s", result.BottleneckType)
	}
}

func TestRound2(t *testing.T) {
	tests := []struct {
		input float64
		want  float64
	}{
		{1.234, 1.23},
		{1.235, 1.24},
		{0.001, 0.0},
		{100.0, 100.0},
	}
	for _, tt := range tests {
		got := round2(tt.input)
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("round2(%f) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func strPtr(s string) *string { return &s }

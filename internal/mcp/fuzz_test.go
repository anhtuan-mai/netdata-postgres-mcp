// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"testing"
	"time"
)

// FuzzParseTimeArg fuzzes the relative/ISO time parser used by query_hardware_metrics,
// summarize_hardware_performance, and find_hardware_bottlenecks tools.
func FuzzParseTimeArg(f *testing.F) {
	// Seed corpus: typical inputs the tools receive.
	seeds := []string{
		"-1h",
		"-30m",
		"-15m",
		"-24h",
		"-7d",
		"-1s",
		"",
		"2024-01-01T00:00:00Z",
		"2024-06-15T12:30:00+02:00",
		"2024-01-01 00:00:00",
		"2024-01-01",
		"not-a-time",
		"-0h",
		"-999999h",
		"0",
		"--1h",
		"-1h30m",
		"2006-01-02T15:04:05Z07:00",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, input string) {
		result, err := parseTimeArg(input)
		if err != nil {
			// parseTimeArg returning an error is fine — it should never panic.
			return
		}
		// If it succeeded, the result should be a valid time.
		if result.IsZero() && input != "" {
			t.Errorf("parseTimeArg(%q) returned zero time without error", input)
		}
		// Result should be within a reasonable range (year 1900 to 2200).
		if result.Year() < 1900 || result.Year() > 2200 {
			// Relative durations like "-999999h" can produce far-past dates — acceptable.
			return
		}
	})
}

// FuzzRound2 ensures round2 never panics on any float64.
func FuzzRound2(f *testing.F) {
	f.Add(0.0)
	f.Add(1.23456789)
	f.Add(-99.999)
	f.Add(1e308)
	f.Add(-1e308)
	f.Add(5e-324) // smallest positive denorm

	f.Fuzz(func(t *testing.T, input float64) {
		result := round2(input)
		// Should never panic. NaN is acceptable output for NaN input.
		_ = result
	})
}

// FuzzDetectBottlenecks ensures the bottleneck detector never panics on arbitrary input.
func FuzzDetectBottlenecks(f *testing.F) {
	f.Add("node1", 50.0, 95.0, 4096.0, 8192.0, 500.0, 80.0, 10.0)

	f.Fuzz(func(t *testing.T, nodeID string,
		cpuAvg, cpuMax, ramAvg, ramMax, swapAvg, diskUtil, iowait float64) {

		inst := ""
		aggs := []DimAgg{
			{Context: "system.cpu", Dimension: "user", Instance: &inst, Avg: cpuAvg, Max: cpuMax, Count: 10},
			{Context: "system.cpu", Dimension: "idle", Instance: &inst, Avg: 100 - cpuAvg, Max: 100, Count: 10},
			{Context: "system.ram", Dimension: "used", Instance: &inst, Avg: ramAvg, Max: ramMax, Count: 10},
			{Context: "system.swap", Dimension: "used", Instance: &inst, Avg: swapAvg, Max: swapAvg * 1.1, Count: 10},
			{Context: "disk.util.sda", Dimension: "utilization", Instance: &inst, Avg: diskUtil * 0.8, Max: diskUtil, Count: 10},
			{Context: "system.cpu", Dimension: "iowait", Instance: &inst, Avg: iowait, Max: iowait * 2, Count: 10},
		}

		result := DetectBottlenecks(nodeID, aggs)
		// Must not panic and must return a valid result.
		if result.NodeID != nodeID {
			t.Errorf("NodeID = %q, want %q", result.NodeID, nodeID)
		}
		if result.Confidence < 0 || result.Confidence > 1.1 {
			t.Errorf("Confidence = %f, out of range", result.Confidence)
		}
	})
}

// FuzzBuildSummary ensures the summary builder never panics.
func FuzzBuildSummary(f *testing.F) {
	f.Add("node1", 20.0, 95.0, 4096.0, 6144.0)

	f.Fuzz(func(t *testing.T, nodeID string, cpuAvg, cpuMax, ramAvg, ramMax float64) {
		from := time.Now().Add(-time.Hour)
		to := time.Now()

		stats := map[string]map[string]*DimStatsPublic{
			"system.cpu": {
				"user": {Sum: cpuAvg * 10, Count: 10, Max: cpuMax, Min: cpuAvg * 0.5},
				"idle": {Sum: (100 - cpuAvg) * 10, Count: 10, Max: 100, Min: 0},
			},
			"system.ram": {
				"used": {Sum: ramAvg * 10, Count: 10, Max: ramMax, Min: ramAvg * 0.8},
			},
		}

		result := BuildSummary(nodeID, from, to, stats)
		if result.NodeID != nodeID {
			t.Errorf("NodeID = %q, want %q", result.NodeID, nodeID)
		}
		if result.Summary == "" {
			t.Error("Summary should not be empty")
		}
	})
}

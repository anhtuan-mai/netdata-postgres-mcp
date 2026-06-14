// SPDX-License-Identifier: GPL-3.0-or-later

package mcp

import (
	"testing"
	"time"
)

func BenchmarkParseTimeArg_Relative(b *testing.B) {
	for b.Loop() {
		parseTimeArg("-1h")
	}
}

func BenchmarkParseTimeArg_ISO(b *testing.B) {
	for b.Loop() {
		parseTimeArg("2024-06-15T12:30:00Z")
	}
}

func BenchmarkRound2(b *testing.B) {
	for b.Loop() {
		round2(123.456789)
	}
}

func BenchmarkDetectBottlenecks_NoBottleneck(b *testing.B) {
	inst := ""
	aggs := []DimAgg{
		{Context: "system.cpu", Dimension: "user", Instance: &inst, Avg: 20, Max: 40, Count: 100},
		{Context: "system.cpu", Dimension: "system", Instance: &inst, Avg: 5, Max: 10, Count: 100},
		{Context: "system.cpu", Dimension: "idle", Instance: &inst, Avg: 75, Max: 95, Count: 100},
		{Context: "system.ram", Dimension: "used", Instance: &inst, Avg: 4096, Max: 5000, Count: 100},
		{Context: "system.ram", Dimension: "free", Instance: &inst, Avg: 4096, Max: 5000, Count: 100},
	}

	for b.Loop() {
		DetectBottlenecks("bench-node", aggs)
	}
}

func BenchmarkDetectBottlenecks_CPUBottleneck(b *testing.B) {
	inst := ""
	aggs := []DimAgg{
		{Context: "system.cpu", Dimension: "user", Instance: &inst, Avg: 85, Max: 99, Count: 100},
		{Context: "system.cpu", Dimension: "system", Instance: &inst, Avg: 10, Max: 20, Count: 100},
		{Context: "system.cpu", Dimension: "idle", Instance: &inst, Avg: 5, Max: 10, Count: 100},
		{Context: "system.ram", Dimension: "used", Instance: &inst, Avg: 4096, Max: 5000, Count: 100},
	}

	for b.Loop() {
		DetectBottlenecks("bench-node", aggs)
	}
}

func BenchmarkBuildSummary(b *testing.B) {
	from := time.Now().Add(-time.Hour)
	to := time.Now()
	stats := map[string]map[string]*DimStatsPublic{
		"system.cpu": {
			"user":   {Sum: 25000, Count: 1000, Max: 99, Min: 5},
			"system": {Sum: 8000, Count: 1000, Max: 30, Min: 2},
			"idle":   {Sum: 67000, Count: 1000, Max: 95, Min: 1},
			"iowait": {Sum: 500, Count: 1000, Max: 15, Min: 0},
		},
		"system.ram": {
			"used":   {Sum: 4096000, Count: 1000, Max: 6144, Min: 3000},
			"free":   {Sum: 4096000, Count: 1000, Max: 5000, Min: 2000},
			"cached": {Sum: 2048000, Count: 1000, Max: 2500, Min: 1500},
		},
		"disk.util.sda": {
			"utilization": {Sum: 40000, Count: 1000, Max: 88, Min: 5},
		},
	}

	for b.Loop() {
		BuildSummary("bench-node", from, to, stats)
	}
}

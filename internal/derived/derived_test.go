// SPDX-License-Identifier: GPL-3.0-or-later

package derived

import (
	"testing"
	"time"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/config"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"
)

func TestCompute_Ratio(t *testing.T) {
	defs := []config.DerivedContext{
		{
			Name:       "custom.memory_pressure",
			Expression: "A / B * 100",
			InputA:     "system.ram.used",
			InputB:     "system.ram.total",
			Unit:       "percentage",
		},
	}

	now := time.Now().UTC()
	samples := []store.MetricSample{
		{NodeID: "node1", CollectedAt: now, Context: "system.ram", Dimension: "used", Value: 7000},
		{NodeID: "node1", CollectedAt: now, Context: "system.ram", Dimension: "total", Value: 8192},
	}

	result := Compute("node1", defs, samples)
	if len(result) != 1 {
		t.Fatalf("expected 1 derived sample, got %d", len(result))
	}
	if result[0].Context != "custom.memory_pressure" {
		t.Errorf("context = %q, want custom.memory_pressure", result[0].Context)
	}
	expected := 7000.0 / 8192.0 * 100
	if diff := result[0].Value - expected; diff > 0.01 || diff < -0.01 {
		t.Errorf("value = %f, want ~%f", result[0].Value, expected)
	}
}

func TestCompute_MissingInput(t *testing.T) {
	defs := []config.DerivedContext{
		{
			Name:       "custom.missing",
			Expression: "A / B * 100",
			InputA:     "system.ram.used",
			InputB:     "system.ram.nonexistent",
			Unit:       "percentage",
		},
	}

	samples := []store.MetricSample{
		{NodeID: "node1", CollectedAt: time.Now().UTC(), Context: "system.ram", Dimension: "used", Value: 7000},
	}

	result := Compute("node1", defs, samples)
	if len(result) != 0 {
		t.Errorf("expected 0 derived samples for missing input, got %d", len(result))
	}
}

func TestCompute_Simple(t *testing.T) {
	defs := []config.DerivedContext{
		{
			Name:       "custom.cpu_user_pct",
			Expression: "A",
			InputA:     "system.cpu.user",
			Unit:       "percentage",
		},
	}

	samples := []store.MetricSample{
		{NodeID: "node1", CollectedAt: time.Now().UTC(), Context: "system.cpu", Dimension: "user", Value: 42.5},
	}

	result := Compute("node1", defs, samples)
	if len(result) != 1 {
		t.Fatalf("expected 1 derived sample, got %d", len(result))
	}
	if result[0].Value != 42.5 {
		t.Errorf("value = %f, want 42.5", result[0].Value)
	}
}

func TestCompute_DivisionByZero(t *testing.T) {
	defs := []config.DerivedContext{
		{
			Name:       "custom.div_zero",
			Expression: "A / B",
			InputA:     "system.ram.used",
			InputB:     "system.ram.free",
			Unit:       "ratio",
		},
	}

	samples := []store.MetricSample{
		{NodeID: "node1", CollectedAt: time.Now().UTC(), Context: "system.ram", Dimension: "used", Value: 100},
		{NodeID: "node1", CollectedAt: time.Now().UTC(), Context: "system.ram", Dimension: "free", Value: 0},
	}

	result := Compute("node1", defs, samples)
	if len(result) != 0 {
		t.Errorf("expected 0 results for div-by-zero, got %d", len(result))
	}
}

func TestCompute_NoDefs(t *testing.T) {
	result := Compute("node1", nil, nil)
	if result != nil {
		t.Errorf("expected nil for no defs, got %v", result)
	}
}

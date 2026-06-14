// SPDX-License-Identifier: GPL-3.0-or-later

// Package derived computes user-defined derived metrics from collected samples.
package derived

import (
	"strings"
	"time"

	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/config"
	"github.com/netdata/netdata/contrib/netdata-postgres-mcp/internal/store"
)

// Compute evaluates derived context definitions against collected samples
// and returns new synthetic samples. Each DerivedContext references inputs
// like "system.ram.used" (context.dimension) and produces a new metric.
func Compute(nodeID string, definitions []config.DerivedContext, samples []store.MetricSample) []store.MetricSample {
	if len(definitions) == 0 {
		return nil
	}

	// Index samples by context.dimension for O(1) lookup.
	latest := map[string]store.MetricSample{}
	for _, s := range samples {
		key := s.Context + "." + s.Dimension
		if existing, ok := latest[key]; !ok || s.CollectedAt.After(existing.CollectedAt) {
			latest[key] = s
		}
	}

	var result []store.MetricSample
	now := time.Now().UTC()

	for _, def := range definitions {
		sampleA, okA := latest[def.InputA]
		if !okA {
			continue // missing required input
		}

		var value float64
		switch strings.ToLower(def.Expression) {
		case "a":
			value = sampleA.Value
		case "a * 100":
			value = sampleA.Value * 100
		case "a / b * 100":
			sampleB, okB := latest[def.InputB]
			if !okB || sampleB.Value == 0 {
				continue
			}
			value = sampleA.Value / sampleB.Value * 100
		case "a / b":
			sampleB, okB := latest[def.InputB]
			if !okB || sampleB.Value == 0 {
				continue
			}
			value = sampleA.Value / sampleB.Value
		case "a - b":
			sampleB, okB := latest[def.InputB]
			if !okB {
				continue
			}
			value = sampleA.Value - sampleB.Value
		case "a + b":
			sampleB, okB := latest[def.InputB]
			if !okB {
				continue
			}
			value = sampleA.Value + sampleB.Value
		default:
			continue // unsupported expression
		}

		result = append(result, store.MetricSample{
			NodeID:      nodeID,
			CollectedAt: now,
			Context:     def.Name,
			Chart:       def.Name,
			Dimension:   "value",
			Unit:        def.Unit,
			Value:       value,
			Labels:      map[string]string{"derived": "true"},
		})
	}

	return result
}

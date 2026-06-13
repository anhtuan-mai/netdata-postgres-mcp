// SPDX-License-Identifier: GPL-3.0-or-later

package collector

import (
	"log/slog"
	"testing"
	"time"
)

func TestParsePrometheusLine(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name      string
		line      string
		wantOK    bool
		wantDim   string
		wantValue float64
		wantChart string
	}{
		{
			name:      "valid line with labels",
			line:      `netdata_system_cpu_percentage_average{chart="system.cpu",dimension="user",family="cpu",instance="localhost"} 12.5`,
			wantOK:    true,
			wantDim:   "user",
			wantValue: 12.5,
			wantChart: "system.cpu",
		},
		{
			name:      "line without labels",
			line:      `netdata_system_cpu 42.0`,
			wantOK:    true,
			wantDim:   "netdata_system_cpu",
			wantValue: 42.0,
		},
		{
			name:   "NaN value",
			line:   `netdata_system_cpu_percentage_average{chart="system.cpu",dimension="idle"} NaN`,
			wantOK: false,
		},
		{
			name:   "empty line",
			line:   "",
			wantOK: false,
		},
		{
			name:   "comment line",
			line:   "# HELP netdata_system_cpu CPU usage",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sample, ok := parsePrometheusLine(tt.line, "test-node", ts)
			if ok != tt.wantOK {
				t.Errorf("parsePrometheusLine() ok = %v, want %v", ok, tt.wantOK)
				return
			}
			if !ok {
				return
			}
			if sample.Dimension != tt.wantDim {
				t.Errorf("dimension = %q, want %q", sample.Dimension, tt.wantDim)
			}
			if sample.Value != tt.wantValue {
				t.Errorf("value = %f, want %f", sample.Value, tt.wantValue)
			}
			if tt.wantChart != "" && sample.Chart != tt.wantChart {
				t.Errorf("chart = %q, want %q", sample.Chart, tt.wantChart)
			}
			if sample.NodeID != "test-node" {
				t.Errorf("nodeID = %q, want %q", sample.NodeID, "test-node")
			}
		})
	}
}

func TestParseJSONResponse_GroupBy(t *testing.T) {
	c := New("http://localhost:19999", "", "test-node", []string{"system.cpu"}, 60, slog.Default())
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// Simulate group_by response
	body := []byte(`{
		"result": {
			"system.cpu,user,,localhost": {
				"labels": {"context": "system.cpu", "dimension": "user"},
				"point": [15.3]
			},
			"system.cpu,system,,localhost": {
				"labels": {"context": "system.cpu", "dimension": "system"},
				"point": [3.2]
			}
		}
	}`)

	samples, err := c.parseJSONResponse(body, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 samples, got %d", len(samples))
	}

	// Check we got both dimensions
	dims := map[string]float64{}
	for _, s := range samples {
		dims[s.Dimension] = s.Value
		if s.NodeID != "test-node" {
			t.Errorf("unexpected nodeID: %s", s.NodeID)
		}
		if s.Context != "system.cpu" {
			t.Errorf("unexpected context: %s", s.Context)
		}
	}
	if v, ok := dims["user"]; !ok || v != 15.3 {
		t.Errorf("missing or wrong user dimension: %v", dims)
	}
	if v, ok := dims["system"]; !ok || v != 3.2 {
		t.Errorf("missing or wrong system dimension: %v", dims)
	}
}

func TestParseJSONResponse_FlatData(t *testing.T) {
	c := New("http://localhost:19999", "", "test-node", []string{"system.cpu"}, 60, slog.Default())
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	body := []byte(`{
		"labels": ["time", "user", "system", "idle"],
		"data": [[1704067200, 10.5, 3.2, 86.3]]
	}`)

	samples, err := c.parseJSONResponse(body, ts)
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) != 3 {
		t.Fatalf("expected 3 samples, got %d", len(samples))
	}
}

func TestSplitLabels(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{`chart="system.cpu",dimension="user",family="cpu"`, 3},
		{`key="value with, comma"`, 1},
		{``, 0},
		{`a="1",b="2"`, 2},
	}

	for _, tt := range tests {
		got := splitLabels(tt.input)
		if len(got) != tt.want {
			t.Errorf("splitLabels(%q) = %d items, want %d; got %v", tt.input, len(got), tt.want, got)
		}
	}
}

func TestPrometheusNameToContext(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"netdata_system_cpu_percentage_average", "netdata_system_cpu"},
		{"netdata_disk_io_kilobytes", "netdata_disk_io"},
		{"netdata_system_ram_megabytes", "netdata_system_ram"},
		{"simple_metric", "simple_metric"},
	}

	for _, tt := range tests {
		got := prometheusNameToContext(tt.input)
		if got != tt.want {
			t.Errorf("prometheusNameToContext(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input interface{}
		want  float64
		ok    bool
	}{
		{float64(42.5), 42.5, true},
		{float32(3.14), float64(float32(3.14)), true},
		{int(10), 10.0, true},
		{int64(100), 100.0, true},
		{"not a number", 0, false},
		{nil, 0, false},
	}

	for _, tt := range tests {
		got, ok := toFloat64(tt.input)
		if ok != tt.ok {
			t.Errorf("toFloat64(%v) ok = %v, want %v", tt.input, ok, tt.ok)
		}
		if ok && got != tt.want {
			t.Errorf("toFloat64(%v) = %f, want %f", tt.input, got, tt.want)
		}
	}
}

func TestParsePrometheusResponse_Filtering(t *testing.T) {
	c := New("http://localhost:19999", "", "test-node",
		[]string{"system.cpu"}, 60, slog.Default())
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	body := []byte(`# HELP netdata_system_cpu CPU
# TYPE netdata_system_cpu gauge
netdata_system_cpu_percentage_average{chart="system.cpu",dimension="user",family="cpu"} 10.0
netdata_system_cpu_percentage_average{chart="system.cpu",dimension="system",family="cpu"} 5.0
netdata_disk_io_kilobytes_average{chart="disk.io",dimension="reads",family="sda"} 100.0
`)

	samples, err := c.parsePrometheusResponse(body, ts)
	if err != nil {
		t.Fatal(err)
	}
	// Should only have system.cpu metrics, disk.io is not in enabled contexts
	for _, s := range samples {
		if s.Context != "" && !contains(s.Context, "system_cpu") && !contains(s.Context, "system.cpu") {
			t.Errorf("unexpected context in filtered results: %s", s.Context)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

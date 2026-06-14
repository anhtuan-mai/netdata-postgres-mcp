// SPDX-License-Identifier: GPL-3.0-or-later

package collector

import (
	"testing"
	"time"
)

func BenchmarkParsePrometheusLine(b *testing.B) {
	line := `netdata_system_cpu_percentage_average{chart="system.cpu",dimension="user",family="cpu",instance="",units="percentage"} 23.456`
	ts := time.Now().UTC()

	for b.Loop() {
		parsePrometheusLine(line, "bench-node", ts)
	}
}

func BenchmarkParsePrometheusLine_NoLabels(b *testing.B) {
	line := `go_goroutines 42`
	ts := time.Now().UTC()

	for b.Loop() {
		parsePrometheusLine(line, "bench-node", ts)
	}
}

func BenchmarkSplitLabels(b *testing.B) {
	labels := `chart="system.cpu",dimension="user",family="cpu",instance="",units="percentage"`

	for b.Loop() {
		splitLabels(labels)
	}
}

func BenchmarkPrometheusNameToContext(b *testing.B) {
	for b.Loop() {
		prometheusNameToContext("netdata_system_cpu_percentage_average")
	}
}

func BenchmarkParseJSONResponse(b *testing.B) {
	c := New("http://localhost:19999", "", "bench-node", []string{"system.cpu"}, 60, nil)
	body := []byte(`{"result":{"system.cpu,user,node1,":{"labels":{},"point":[23.45]},"system.cpu,system,node1,":{"labels":{},"point":[12.34]},"system.cpu,idle,node1,":{"labels":{},"point":[64.21]}}}`)
	ts := time.Now().UTC()

	for b.Loop() {
		c.parseJSONResponse(body, ts)
	}
}

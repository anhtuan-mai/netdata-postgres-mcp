// SPDX-License-Identifier: GPL-3.0-or-later

// Package metrics provides lightweight Prometheus-compatible counters and
// histograms for self-observability. No external dependencies — metrics are
// served as plain text in Prometheus exposition format.
package metrics

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds self-observability counters for the sidecar.
type Metrics struct {
	CollectionTotal    atomic.Int64
	CollectionErrors   atomic.Int64
	SamplesInserted    atomic.Int64
	RetentionDeleted   atomic.Int64
	CollectionDuration syncDuration
}

type syncDuration struct {
	mu    sync.Mutex
	last  time.Duration
	total time.Duration
	count int64
}

func (d *syncDuration) Record(dur time.Duration) {
	d.mu.Lock()
	d.last = dur
	d.total += dur
	d.count++
	d.mu.Unlock()
}

func (d *syncDuration) Snapshot() (last, avg time.Duration, count int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.count == 0 {
		return 0, 0, 0
	}
	return d.last, d.total / time.Duration(d.count), d.count
}

// Global is the singleton metrics instance.
var Global = &Metrics{}

// Handler returns an http.HandlerFunc that serves /metrics in Prometheus
// exposition format.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m := Global
		last, avg, count := m.CollectionDuration.Snapshot()

		var b strings.Builder
		fmt.Fprintf(&b, "# HELP netdata_mcp_collections_total Total number of collection cycles.\n")
		fmt.Fprintf(&b, "# TYPE netdata_mcp_collections_total counter\n")
		fmt.Fprintf(&b, "netdata_mcp_collections_total %d\n\n", m.CollectionTotal.Load())

		fmt.Fprintf(&b, "# HELP netdata_mcp_collection_errors_total Total number of failed collection cycles.\n")
		fmt.Fprintf(&b, "# TYPE netdata_mcp_collection_errors_total counter\n")
		fmt.Fprintf(&b, "netdata_mcp_collection_errors_total %d\n\n", m.CollectionErrors.Load())

		fmt.Fprintf(&b, "# HELP netdata_mcp_samples_inserted_total Total number of metric samples inserted into PostgreSQL.\n")
		fmt.Fprintf(&b, "# TYPE netdata_mcp_samples_inserted_total counter\n")
		fmt.Fprintf(&b, "netdata_mcp_samples_inserted_total %d\n\n", m.SamplesInserted.Load())

		fmt.Fprintf(&b, "# HELP netdata_mcp_retention_deleted_total Total number of expired samples deleted by retention cleanup.\n")
		fmt.Fprintf(&b, "# TYPE netdata_mcp_retention_deleted_total counter\n")
		fmt.Fprintf(&b, "netdata_mcp_retention_deleted_total %d\n\n", m.RetentionDeleted.Load())

		fmt.Fprintf(&b, "# HELP netdata_mcp_collection_duration_seconds Duration of the last collection cycle in seconds.\n")
		fmt.Fprintf(&b, "# TYPE netdata_mcp_collection_duration_seconds gauge\n")
		fmt.Fprintf(&b, "netdata_mcp_collection_duration_seconds{stat=\"last\"} %.6f\n", last.Seconds())
		fmt.Fprintf(&b, "netdata_mcp_collection_duration_seconds{stat=\"avg\"} %.6f\n", avg.Seconds())
		fmt.Fprintf(&b, "netdata_mcp_collection_duration_seconds{stat=\"count\"} %d\n\n", count)

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(b.String()))
	}
}

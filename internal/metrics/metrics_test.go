// SPDX-License-Identifier: GPL-3.0-or-later

package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetrics_AtomicCounters(t *testing.T) {
	m := &Metrics{}

	m.CollectionTotal.Add(5)
	m.CollectionErrors.Add(2)
	m.SamplesInserted.Add(1000)
	m.RetentionDeleted.Add(50)

	if got := m.CollectionTotal.Load(); got != 5 {
		t.Errorf("CollectionTotal = %d, want 5", got)
	}
	if got := m.CollectionErrors.Load(); got != 2 {
		t.Errorf("CollectionErrors = %d, want 2", got)
	}
	if got := m.SamplesInserted.Load(); got != 1000 {
		t.Errorf("SamplesInserted = %d, want 1000", got)
	}
	if got := m.RetentionDeleted.Load(); got != 50 {
		t.Errorf("RetentionDeleted = %d, want 50", got)
	}
}

func TestSyncDuration_RecordAndSnapshot(t *testing.T) {
	var d syncDuration

	// Empty state
	last, avg, count := d.Snapshot()
	if last != 0 || avg != 0 || count != 0 {
		t.Errorf("empty snapshot: last=%v avg=%v count=%v", last, avg, count)
	}

	// Record values
	d.Record(100 * time.Millisecond)
	d.Record(200 * time.Millisecond)
	d.Record(300 * time.Millisecond)

	last, avg, count = d.Snapshot()
	if last != 300*time.Millisecond {
		t.Errorf("last = %v, want 300ms", last)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
	// avg should be (100+200+300)/3 = 200ms
	if avg != 200*time.Millisecond {
		t.Errorf("avg = %v, want 200ms", avg)
	}
}

func TestHandler_PrometheusFormat(t *testing.T) {
	// Reset global metrics for this test
	old := Global
	Global = &Metrics{}
	defer func() { Global = old }()

	Global.CollectionTotal.Add(10)
	Global.CollectionErrors.Add(1)
	Global.SamplesInserted.Add(500)
	Global.RetentionDeleted.Add(25)
	Global.CollectionDuration.Record(150 * time.Millisecond)

	handler := Handler()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain", ct)
	}

	body := rec.Body.String()

	// Check all expected metrics are present
	expected := []string{
		"netdata_mcp_collections_total 10",
		"netdata_mcp_collection_errors_total 1",
		"netdata_mcp_samples_inserted_total 500",
		"netdata_mcp_retention_deleted_total 25",
		"netdata_mcp_collection_duration_seconds{stat=\"last\"} 0.15",
		"# TYPE netdata_mcp_collections_total counter",
		"# HELP netdata_mcp_collections_total",
	}
	for _, exp := range expected {
		if !strings.Contains(body, exp) {
			t.Errorf("body missing %q\ngot:\n%s", exp, body)
		}
	}
}

func TestGlobal_NotNil(t *testing.T) {
	if Global == nil {
		t.Fatal("Global metrics singleton should not be nil")
	}
}

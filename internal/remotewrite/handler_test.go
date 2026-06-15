// SPDX-License-Identifier: GPL-3.0-or-later

package remotewrite

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestParseWriteRequest_Empty(t *testing.T) {
	result, err := parseWriteRequest(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 0 {
		t.Fatalf("expected 0 timeseries, got %d", len(result))
	}
}

func TestParseWriteRequest_SingleSeries(t *testing.T) {
	labelMsg := encodeTestString(1, "__name__")
	labelMsg = append(labelMsg, encodeTestString(2, "cpu_total")...)

	sampleMsg := encodeTestFixed64(1, math.Float64bits(42.5))
	sampleMsg = append(sampleMsg, encodeTestVarintField(2, 1000)...)

	tsMsg := encodeTestLenDelim(1, labelMsg)
	tsMsg = append(tsMsg, encodeTestLenDelim(2, sampleMsg)...)

	writeReq := encodeTestLenDelim(1, tsMsg)

	result, err := parseWriteRequest(writeReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 timeseries, got %d", len(result))
	}
	if len(result[0].Labels) != 1 {
		t.Fatalf("expected 1 label, got %d", len(result[0].Labels))
	}
	if result[0].Labels[0].Name != "__name__" || result[0].Labels[0].Value != "cpu_total" {
		t.Fatalf("unexpected label: %+v", result[0].Labels[0])
	}
	if len(result[0].Samples) != 1 {
		t.Fatalf("expected 1 sample, got %d", len(result[0].Samples))
	}
	if result[0].Samples[0].Value != 42.5 {
		t.Fatalf("expected value 42.5, got %f", result[0].Samples[0].Value)
	}
	if result[0].Samples[0].TimestampMs != 1000 {
		t.Fatalf("expected ts 1000, got %d", result[0].Samples[0].TimestampMs)
	}
}

func TestParseWriteRequest_MultipleLabels(t *testing.T) {
	label1 := encodeTestString(1, "__name__")
	label1 = append(label1, encodeTestString(2, "http_requests")...)

	label2 := encodeTestString(1, "method")
	label2 = append(label2, encodeTestString(2, "GET")...)

	tsMsg := encodeTestLenDelim(1, label1)
	tsMsg = append(tsMsg, encodeTestLenDelim(1, label2)...)

	sampleMsg := encodeTestFixed64(1, math.Float64bits(100.0))
	sampleMsg = append(sampleMsg, encodeTestVarintField(2, 2000)...)
	tsMsg = append(tsMsg, encodeTestLenDelim(2, sampleMsg)...)

	writeReq := encodeTestLenDelim(1, tsMsg)

	result, err := parseWriteRequest(writeReq)
	if err != nil {
		t.Fatal(err)
	}
	if len(result[0].Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(result[0].Labels))
	}
}

func TestMetricToContext(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"node_cpu_seconds_total", "system.cpu"},
		{"node_disk_read_bytes", "disk.io"},
		{"node_memory_MemTotal", "system.ram"},
		{"node_network_receive_bytes", "system.ip"},
		{"go_goroutines", "prometheus.go_goroutines"},
	}
	for _, tt := range tests {
		if got := metricToContext(tt.input); got != tt.want {
			t.Errorf("metricToContext(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// helpers for building protobuf test data
func encodeTestVarint(v uint64) []byte {
	var buf [10]byte
	n := binary.PutUvarint(buf[:], v)
	return buf[:n]
}

func encodeTestTag(field, wireType int) []byte {
	return encodeTestVarint(uint64(field<<3 | wireType))
}

func encodeTestString(field int, s string) []byte {
	tag := encodeTestTag(field, 2)
	length := encodeTestVarint(uint64(len(s)))
	result := make([]byte, 0, len(tag)+len(length)+len(s))
	result = append(result, tag...)
	result = append(result, length...)
	result = append(result, []byte(s)...)
	return result
}

func encodeTestFixed64(field int, v uint64) []byte {
	tag := encodeTestTag(field, 1)
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	result := make([]byte, 0, len(tag)+8)
	result = append(result, tag...)
	result = append(result, buf[:]...)
	return result
}

func encodeTestVarintField(field int, v uint64) []byte {
	tag := encodeTestTag(field, 0)
	val := encodeTestVarint(v)
	result := make([]byte, 0, len(tag)+len(val))
	result = append(result, tag...)
	result = append(result, val...)
	return result
}

func encodeTestLenDelim(field int, data []byte) []byte {
	tag := encodeTestTag(field, 2)
	length := encodeTestVarint(uint64(len(data)))
	result := make([]byte, 0, len(tag)+len(length)+len(data))
	result = append(result, tag...)
	result = append(result, length...)
	result = append(result, data...)
	return result
}

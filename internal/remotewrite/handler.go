// SPDX-License-Identifier: GPL-3.0-or-later

// Package remotewrite implements a lightweight Prometheus remote-write receiver.
// It accepts snappy-compressed protobuf payloads and inserts samples into PostgreSQL.
// Uses a minimal hand-rolled protobuf decoder to avoid importing the full
// prometheus/prometheus module.
package remotewrite

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/golang/snappy"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Handler implements http.Handler for the Prometheus remote-write protocol.
type Handler struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
	nodeID string
}

// NewHandler creates a new remote-write HTTP handler.
func NewHandler(pool *pgxpool.Pool, nodeID string, logger *slog.Logger) *Handler {
	return &Handler{pool: pool, nodeID: nodeID, logger: logger}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 10<<20))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}

	decoded, err := snappy.Decode(nil, body)
	if err != nil {
		http.Error(w, "snappy decode error", http.StatusBadRequest)
		return
	}

	timeseries, err := parseWriteRequest(decoded)
	if err != nil {
		http.Error(w, "protobuf decode error", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	inserted, err := h.ingest(ctx, timeseries)
	if err != nil {
		h.logger.Error("remote-write ingest error", "error", err)
		http.Error(w, "ingest error", http.StatusInternalServerError)
		return
	}

	h.logger.Debug("remote-write ingested", "samples", inserted)
	w.WriteHeader(http.StatusNoContent)
}

type prSample struct {
	TimestampMs int64
	Value       float64
}

type prLabel struct {
	Name  string
	Value string
}

type prTimeSeries struct {
	Labels  []prLabel
	Samples []prSample
}

func (h *Handler) ingest(ctx context.Context, series []prTimeSeries) (int, error) {
	count := 0
	for _, ts := range series {
		metricName := ""
		instance := ""
		for _, l := range ts.Labels {
			if l.Name == "__name__" {
				metricName = l.Value
			} else if l.Name == "instance" {
				instance = l.Value
			}
		}
		if metricName == "" {
			continue
		}

		metricContext := metricToContext(metricName)
		dimension := metricName

		for _, s := range ts.Samples {
			if math.IsNaN(s.Value) || math.IsInf(s.Value, 0) {
				continue
			}
			collectedAt := time.Unix(0, s.TimestampMs*int64(time.Millisecond)).UTC()
			_, err := h.pool.Exec(ctx, `
				INSERT INTO hardware_metric_samples
					(node_id, collected_at, context, dimension, instance, value)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, h.nodeID, collectedAt, metricContext, dimension, instance, s.Value)
			if err != nil {
				return count, fmt.Errorf("insert sample: %w", err)
			}
			count++
		}
	}
	return count, nil
}

func metricToContext(name string) string {
	switch {
	case strings.HasPrefix(name, "node_cpu"):
		return "system.cpu"
	case strings.HasPrefix(name, "node_disk"):
		return "disk.io"
	case strings.HasPrefix(name, "node_memory"):
		return "system.ram"
	case strings.HasPrefix(name, "node_network"):
		return "system.ip"
	default:
		return "prometheus." + name
	}
}

// parseWriteRequest decodes a minimal Prometheus WriteRequest protobuf.
func parseWriteRequest(data []byte) ([]prTimeSeries, error) {
	var result []prTimeSeries
	for len(data) > 0 {
		fieldNum, wireType, n := decodeTag(data)
		if n == 0 {
			return nil, fmt.Errorf("invalid tag")
		}
		data = data[n:]

		if fieldNum == 1 && wireType == 2 {
			msgLen, n := decodeVarint(data)
			if n == 0 {
				return nil, fmt.Errorf("invalid length")
			}
			data = data[n:]
			if int(msgLen) > len(data) {
				return nil, fmt.Errorf("truncated message")
			}
			ts, err := parseTimeSeries(data[:msgLen])
			if err != nil {
				return nil, err
			}
			result = append(result, ts)
			data = data[msgLen:]
		} else {
			skip, err := skipField(wireType, data)
			if err != nil {
				return nil, err
			}
			data = data[skip:]
		}
	}
	return result, nil
}

func parseTimeSeries(data []byte) (prTimeSeries, error) {
	var ts prTimeSeries
	for len(data) > 0 {
		fieldNum, wireType, n := decodeTag(data)
		if n == 0 {
			return ts, fmt.Errorf("invalid tag in timeseries")
		}
		data = data[n:]

		if wireType == 2 {
			msgLen, n := decodeVarint(data)
			if n == 0 {
				return ts, fmt.Errorf("invalid length in timeseries")
			}
			data = data[n:]
			if int(msgLen) > len(data) {
				return ts, fmt.Errorf("truncated timeseries field")
			}
			switch fieldNum {
			case 1:
				l, err := parseLabel(data[:msgLen])
				if err != nil {
					return ts, err
				}
				ts.Labels = append(ts.Labels, l)
			case 2:
				s, err := parseSample(data[:msgLen])
				if err != nil {
					return ts, err
				}
				ts.Samples = append(ts.Samples, s)
			}
			data = data[msgLen:]
		} else {
			skip, err := skipField(wireType, data)
			if err != nil {
				return ts, err
			}
			data = data[skip:]
		}
	}
	return ts, nil
}

func parseLabel(data []byte) (prLabel, error) {
	var l prLabel
	for len(data) > 0 {
		fieldNum, wireType, n := decodeTag(data)
		if n == 0 {
			return l, fmt.Errorf("invalid tag in label")
		}
		data = data[n:]

		if wireType == 2 {
			strLen, n := decodeVarint(data)
			if n == 0 {
				return l, fmt.Errorf("invalid string length")
			}
			data = data[n:]
			if int(strLen) > len(data) {
				return l, fmt.Errorf("truncated string")
			}
			switch fieldNum {
			case 1:
				l.Name = string(data[:strLen])
			case 2:
				l.Value = string(data[:strLen])
			}
			data = data[strLen:]
		} else {
			skip, err := skipField(wireType, data)
			if err != nil {
				return l, err
			}
			data = data[skip:]
		}
	}
	return l, nil
}

func parseSample(data []byte) (prSample, error) {
	var s prSample
	for len(data) > 0 {
		fieldNum, wireType, n := decodeTag(data)
		if n == 0 {
			return s, fmt.Errorf("invalid tag in sample")
		}
		data = data[n:]

		switch {
		case fieldNum == 1 && wireType == 1:
			if len(data) < 8 {
				return s, fmt.Errorf("truncated double")
			}
			bits := binary.LittleEndian.Uint64(data[:8])
			s.Value = math.Float64frombits(bits)
			data = data[8:]
		case fieldNum == 2 && wireType == 0:
			v, n := decodeVarint(data)
			if n == 0 {
				return s, fmt.Errorf("invalid varint for timestamp")
			}
			s.TimestampMs = int64(v)
			data = data[n:]
		default:
			skip, err := skipField(wireType, data)
			if err != nil {
				return s, err
			}
			data = data[skip:]
		}
	}
	return s, nil
}

func decodeTag(data []byte) (fieldNum int, wireType int, n int) {
	v, n := decodeVarint(data)
	if n == 0 {
		return 0, 0, 0
	}
	return int(v >> 3), int(v & 0x7), n
}

func decodeVarint(data []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, b := range data {
		if i >= 10 {
			return 0, 0
		}
		if b < 0x80 {
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0
}

func skipField(wireType int, data []byte) (int, error) {
	switch wireType {
	case 0:
		_, n := decodeVarint(data)
		if n == 0 {
			return 0, fmt.Errorf("invalid varint skip")
		}
		return n, nil
	case 1:
		if len(data) < 8 {
			return 0, fmt.Errorf("truncated fixed64")
		}
		return 8, nil
	case 2:
		l, n := decodeVarint(data)
		if n == 0 {
			return 0, fmt.Errorf("invalid length skip")
		}
		return n + int(l), nil
	case 5:
		if len(data) < 4 {
			return 0, fmt.Errorf("truncated fixed32")
		}
		return 4, nil
	default:
		return 0, fmt.Errorf("unknown wire type %d", wireType)
	}
}

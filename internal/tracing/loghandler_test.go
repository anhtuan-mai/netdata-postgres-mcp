// SPDX-License-Identifier: GPL-3.0-or-later

package tracing

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestLogHandler_NoSpan(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := NewLogHandler(inner)
	logger := slog.New(handler)

	logger.Info("test message")

	output := buf.String()
	if strings.Contains(output, "trace_id") {
		t.Errorf("should not contain trace_id without active span: %s", output)
	}
}

func TestLogHandler_WithSpan(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := NewLogHandler(inner)
	logger := slog.New(handler)

	// Create a fake span context
	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	spanID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	})

	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	logger.InfoContext(ctx, "test with span")

	output := buf.String()
	if !strings.Contains(output, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Errorf("should contain trace_id: %s", output)
	}
	if !strings.Contains(output, "00f067aa0ba902b7") {
		t.Errorf("should contain span_id: %s", output)
	}
}

func TestLogHandler_Enabled(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, &slog.HandlerOptions{Level: slog.LevelWarn})
	handler := NewLogHandler(inner)

	if handler.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("info should not be enabled when level is warn")
	}
	if !handler.Enabled(context.Background(), slog.LevelWarn) {
		t.Error("warn should be enabled when level is warn")
	}
}

func TestLogHandler_WithAttrs(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	handler := NewLogHandler(inner)

	withAttrs := handler.WithAttrs([]slog.Attr{slog.String("key", "val")})
	if _, ok := withAttrs.(*LogHandler); !ok {
		t.Error("WithAttrs should return a *LogHandler")
	}
}

func TestLogHandler_WithGroup(t *testing.T) {
	inner := slog.NewTextHandler(&bytes.Buffer{}, nil)
	handler := NewLogHandler(inner)

	withGroup := handler.WithGroup("mygroup")
	if _, ok := withGroup.(*LogHandler); !ok {
		t.Error("WithGroup should return a *LogHandler")
	}
}

// Ensure noop tracer doesn't inject trace fields.
func TestLogHandler_NoopTracer(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	handler := NewLogHandler(inner)
	logger := slog.New(handler)

	tp := noop.NewTracerProvider()
	tracer := tp.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "noop-span")
	defer span.End()

	logger.InfoContext(ctx, "noop test")

	output := buf.String()
	// noop span has zero trace ID — SpanContext().IsValid() returns false
	if strings.Contains(output, "trace_id") {
		t.Errorf("noop tracer should not inject trace_id: %s", output)
	}
}

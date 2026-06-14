// SPDX-License-Identifier: GPL-3.0-or-later

package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// LogHandler wraps a slog.Handler to inject trace_id and span_id from the
// context into every log record. This enables log-to-trace correlation in
// observability backends (Grafana Tempo, Jaeger, etc.).
type LogHandler struct {
	inner slog.Handler
}

// NewLogHandler wraps an existing slog.Handler with trace ID injection.
func NewLogHandler(inner slog.Handler) *LogHandler {
	return &LogHandler{inner: inner}
}

// Enabled delegates to the inner handler.
func (h *LogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

// Handle adds trace_id and span_id attributes if a span is active in the context.
func (h *LogHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		record.AddAttrs(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, record)
}

// WithAttrs returns a new handler with the given attributes.
func (h *LogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &LogHandler{inner: h.inner.WithAttrs(attrs)}
}

// WithGroup returns a new handler with the given group name.
func (h *LogHandler) WithGroup(name string) slog.Handler {
	return &LogHandler{inner: h.inner.WithGroup(name)}
}

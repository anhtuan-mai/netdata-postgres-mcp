// SPDX-License-Identifier: GPL-3.0-or-later

// Package tracing provides optional OpenTelemetry-compatible distributed
// tracing. When OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is a no-op.
//
// This package uses the standard OpenTelemetry Go SDK. To keep the binary
// lean, tracing is opt-in: if no OTLP endpoint is configured, Init() returns
// a no-op tracer and zero-cost span wrappers.
package tracing

import (
	"context"
	"log/slog"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "netdata-postgres-mcp"

// Init sets up the OpenTelemetry tracer provider. If OTEL_EXPORTER_OTLP_ENDPOINT
// is not set, it configures a no-op tracer. Returns a shutdown function.
func Init(ctx context.Context, version string, logger *slog.Logger) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		logger.Debug("tracing disabled — OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(tracerName),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)

	logger.Info("tracing enabled", "endpoint", endpoint)

	shutdown := func(ctx context.Context) error {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(shutdownCtx)
	}
	return shutdown, nil
}

// Tracer returns the package-level tracer.
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}

// StartSpan starts a new span with the given name and optional attributes.
func StartSpan(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	return Tracer().Start(ctx, name, trace.WithAttributes(attrs...))
}

// RecordError records an error on the span in the context.
func RecordError(ctx context.Context, err error) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.RecordError(err)
	}
}

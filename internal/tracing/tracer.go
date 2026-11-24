package tracing

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is the package-level tracer used for creating spans.
// Initialized to a noop tracer by default (zero overhead). Replaced by
// InitTracing when an OTLP endpoint is configured.
var Tracer trace.Tracer = otel.Tracer("recall-mcp-gateway")

// InitTracing sets up OpenTelemetry tracing with an OTLP HTTP exporter.
// If otlpEndpoint is empty, tracing is disabled (noop tracer, zero overhead).
// Returns a shutdown function that flushes pending spans.
func InitTracing(ctx context.Context, serviceName, otlpEndpoint string) (func(context.Context) error, error) {
	if otlpEndpoint == "" {
		Tracer = otel.Tracer(serviceName)
		slog.Info("tracing disabled (no endpoint configured)")
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(otlpEndpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
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
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	Tracer = tp.Tracer(serviceName)
	slog.Info("tracing initialized", "endpoint", otlpEndpoint, "service", serviceName)

	return tp.Shutdown, nil
}

// ExtractTraceParent parses a W3C traceparent string and returns a context
// with the extracted span context as the remote parent. If traceparent is
// empty or malformed, the returned context is unchanged (no parent).
func ExtractTraceParent(ctx context.Context, traceparent, tracestate string) context.Context {
	if traceparent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{
		"traceparent": traceparent,
	}
	if tracestate != "" {
		carrier["tracestate"] = tracestate
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// TraceIDFromContext returns the trace ID from the current span context, or empty string.
func TraceIDFromContext(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if sc.HasTraceID() {
		return sc.TraceID().String()
	}
	return ""
}

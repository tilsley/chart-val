// Package telemetry initializes OpenTelemetry metrics and tracing.
package telemetry

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

const serviceName = "chart-val"

// Telemetry holds the OTel meter and tracer plus a shutdown function.
type Telemetry struct {
	Meter    metric.Meter
	Tracer   trace.Tracer
	Shutdown func(ctx context.Context) error
}

// New creates a Telemetry instance. When enabled is false, noop
// implementations are returned with zero overhead. When enabled, the OTel SDK
// auto-discovers OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME, etc. from
// the environment.
func New(ctx context.Context, enabled bool) (*Telemetry, error) {
	if !enabled {
		return &Telemetry{
			Meter:    noopmetric.NewMeterProvider().Meter(serviceName),
			Tracer:   nooptrace.NewTracerProvider().Tracer(serviceName),
			Shutdown: func(context.Context) error { return nil },
		}, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, err
	}

	// Trace exporter (OTLP gRPC, auto-discovers endpoint from env)
	traceExp, err := otlptracegrpc.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	// Metric exporter (OTLP gRPC, auto-discovers endpoint from env)
	metricExp, err := otlpmetricgrpc.New(ctx)
	if err != nil {
		_ = tp.Shutdown(ctx)
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(30*time.Second))),
		sdkmetric.WithResource(res),
	)

	// Register as global providers so instrumentation libraries (e.g. otelhttp)
	// automatically pick them up for outbound HTTP spans.
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	shutdown := func(ctx context.Context) error {
		mErr := mp.Shutdown(ctx)
		tErr := tp.Shutdown(ctx)
		if mErr != nil {
			return mErr
		}
		return tErr
	}

	return &Telemetry{
		Meter:    mp.Meter(serviceName),
		Tracer:   tp.Tracer(serviceName),
		Shutdown: shutdown,
	}, nil
}

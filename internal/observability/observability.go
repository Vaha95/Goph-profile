package observability

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
	"strconv"
)

type ShutdownFunc func(ctx context.Context) error

func Init(serviceName, otelEndpoint string, metricsPort int) (ShutdownFunc, error) {
	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	shutdownFuncs := make([]func(context.Context) error, 0)

	tp, err := initTracerProvider(res, otelEndpoint)
	if err != nil {
		return nil, fmt.Errorf("init tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	shutdownFuncs = append(shutdownFuncs, tp.Shutdown)

	mp, err := initMeterProvider(res)
	if err != nil {
		tp.Shutdown(context.Background())
		return nil, fmt.Errorf("init meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)
	shutdownFuncs = append(shutdownFuncs, mp.Shutdown)

	shutdown := func(ctx context.Context) error {
		var firstErr error
		for _, fn := range shutdownFuncs {
			if err := fn(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return firstErr
	}

	return shutdown, nil
}

func initTracerProvider(res *resource.Resource, endpoint string) (*sdktrace.TracerProvider, error) {
	opts := []otlptracehttp.Option{}

	if endpoint != "" {
		opts = append(opts, otlptracehttp.WithEndpoint(endpoint))
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exporter, err := otlptracehttp.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("create OTLP trace exporter: %w", err)
	}

	samplerRatio := 1.0
	if v := os.Getenv("OTEL_SAMPLER_RATIO"); v != "" {
		if r, err := strconv.ParseFloat(v, 64); err == nil && r >= 0 && r <= 1 {
			samplerRatio = r
		}
	}

	var sampler sdktrace.Sampler
	if samplerRatio >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else {
		sampler = sdktrace.TraceIDRatioBased(samplerRatio)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	return tp, nil
}

func initMeterProvider(res *resource.Resource) (*sdkmetric.MeterProvider, error) {
	exporter, err := prometheus.New()
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exporter),
		sdkmetric.WithResource(res),
	)

	return mp, nil
}

func InitLogger(serviceName string, level slog.Level) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)

	logger := slog.New(handler).With(
		slog.String("service", serviceName),
	)

	slog.SetDefault(logger)

	return logger
}

func LoggerWithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
	spanCtx := trace.SpanFromContext(ctx).SpanContext()
	if !spanCtx.IsValid() {
		return logger
	}
	return logger.With(
		slog.String("trace_id", spanCtx.TraceID().String()),
		slog.String("span_id", spanCtx.SpanID().String()),
	)
}

// L returns a logger that includes trace_id and span_id from the context.
func L(ctx context.Context) *slog.Logger {
	return LoggerWithTrace(ctx, slog.Default())
}

// PrometheusHandler returns an HTTP handler that exposes OTel metrics
// via the Prometheus exporter on /metrics.
func PrometheusHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	return mux
}

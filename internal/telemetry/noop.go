package telemetry

import (
	"context"

	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
	oteltrace "go.opentelemetry.io/otel/trace"
	trace_noop "go.opentelemetry.io/otel/trace/noop"
)

type NoopTelemetry struct {
	cfg Config
}

func NewNoopTelemetry(cfg Config) (*NoopTelemetry, error) {
	return &NoopTelemetry{cfg: cfg}, nil
}

func (n *NoopTelemetry) GetServiceName() string {
	return n.cfg.ServiceName
}

func (n *NoopTelemetry) LogInfo(ctx context.Context, msg string, args ...any)       {}
func (n *NoopTelemetry) LogErrorln(ctx context.Context, msg string, args ...any)    {}
func (n *NoopTelemetry) LogFatalln(ctx context.Context, msg string, args ...any)    {}

func (n *NoopTelemetry) MeterInt64Counter(metric Metric) (otelmetric.Int64Counter, error) {
	return noop.NewMeterProvider().Meter(n.cfg.ServiceName).Int64Counter(metric.Name)
}

func (n *NoopTelemetry) MeterInt64Histogram(metric Metric) (otelmetric.Int64Histogram, error) {
	return noop.NewMeterProvider().Meter(n.cfg.ServiceName).Int64Histogram(metric.Name)
}

func (n *NoopTelemetry) MeterInt64UpDownCounter(metric Metric) (otelmetric.Int64UpDownCounter, error) {
	return noop.NewMeterProvider().Meter(n.cfg.ServiceName).Int64UpDownCounter(metric.Name)
}

func (n *NoopTelemetry) TraceStart(ctx context.Context, name string) (context.Context, oteltrace.Span) {
	return trace_noop.NewTracerProvider().Tracer(n.cfg.ServiceName).Start(ctx, name)
}

func (n *NoopTelemetry) SubscribeLogs() chan string {
	ch := make(chan string)
	close(ch)
	return ch
}

func (n *NoopTelemetry) UnsubscribeLogs(ch chan string) {}

func (n *NoopTelemetry) Shutdown(ctx context.Context) {}

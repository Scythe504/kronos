package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	// "net/http"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

type TelemetryProvider interface {
	GetServiceName() string
	LogInfo(msg string, args ...any)
	LogErrorln(msg string, args ...any)
	LogFatalln(msg string, args ...any)
	MeterInt64Counter(metric Metric) (otelmetric.Int64Counter, error)
	MeterInt64Histogram(metric Metric) (otelmetric.Int64Histogram, error)
	MeterInt64UpDownCounter(metric Metric) (otelmetric.Int64UpDownCounter, error)
	TraceStart(ctx context.Context, name string) (context.Context, oteltrace.Span)
	Shutdown(ctx context.Context)
}

type Telemetry struct {
	lp     *log.LoggerProvider
	mp     *metric.MeterProvider
	tp     *trace.TracerProvider
	log    *slog.Logger
	meter  otelmetric.Meter
	tracer oteltrace.Tracer
	cfg    Config
}

func NewTelemetry(ctx context.Context, cfg Config) (*Telemetry, error) {
	rp := newResource(cfg.ServiceName, cfg.ServiceVersion)

	lp, err := newLoggerProvider(ctx, rp)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})

	otelHandler := otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp))

	logger := slog.New(
		slog.NewMultiHandler(jsonHandler, otelHandler),
	)
	slog.SetDefault(logger)

	mp, err := newMeterProvider(ctx, rp)
	if err != nil {
		return nil, fmt.Errorf("failed to create meter: %w", err)
	}

	meter := mp.Meter(cfg.ServiceName)

	tp, err := newTracerProvider(ctx, rp)
	if err != nil {
		return nil, fmt.Errorf("failed to create tracer: %w", err)
	}
	tracer := tp.Tracer(cfg.ServiceName)

	return &Telemetry{
		lp:     lp,
		mp:     mp,
		tp:     tp,
		log:    logger,
		meter:  meter,
		tracer: tracer,
		cfg:    cfg,
	}, nil
}

func (t *Telemetry) GetServiceName() string {
	return t.cfg.ServiceName
}

func (t *Telemetry) LogInfo(msg string, args ...any) {
	t.log.Info(msg, args...)
}

func (t *Telemetry) LogErrorln(msg string, args ...any) {
	t.log.Error(msg, args...)
}

func (t *Telemetry) LogFatalln(msg string, args ...any) {
	t.log.Error(msg, args...)
	os.Exit(1)
}

func (t *Telemetry) MeterInt64Counter(metric Metric) (otelmetric.Int64Counter, error) {
	counter, err := t.meter.Int64Counter(
		metric.Name,
		otelmetric.WithDescription(metric.Description),
		otelmetric.WithUnit(metric.Unit),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create counter: %w", err)
	}

	return counter, nil
}

func (t *Telemetry) MeterInt64Histogram(metric Metric) (otelmetric.Int64Histogram, error) {
	histogram, err := t.meter.Int64Histogram(
		metric.Name,
		otelmetric.WithDescription(metric.Description),
		otelmetric.WithUnit(metric.Unit),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create histogram: %w", err)
	}

	return histogram, nil
}

func (t *Telemetry) MeterInt64UpDownCounter(metric Metric) (otelmetric.Int64UpDownCounter, error) {
	counter, err := t.meter.Int64UpDownCounter(
		metric.Name,
		otelmetric.WithDescription(metric.Description),
		otelmetric.WithUnit(metric.Unit),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to create counter: %w", err)
	}

	return counter, nil
}

func (t *Telemetry) TraceStart(ctx context.Context, name string) (context.Context, oteltrace.Span) {
	return t.tracer.Start(ctx, name)
}

func (t *Telemetry) Shutdown(ctx context.Context) {
	t.lp.Shutdown(ctx)
	t.mp.Shutdown(ctx)
	t.tp.Shutdown(ctx)
}

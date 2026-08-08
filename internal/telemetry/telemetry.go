package telemetry

import (
	"context"
	"fmt"
	"log/slog"
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
	GetConfig() Config
	LogInfo(ctx context.Context, msg string, args ...any)
	LogErrorln(ctx context.Context, msg string, args ...any)
	LogFatalln(ctx context.Context, msg string, args ...any)
	MeterInt64Counter(metric Metric) (otelmetric.Int64Counter, error)
	MeterInt64Histogram(metric Metric) (otelmetric.Int64Histogram, error)
	MeterInt64UpDownCounter(metric Metric) (otelmetric.Int64UpDownCounter, error)
	TraceStart(ctx context.Context, name string) (context.Context, oteltrace.Span)
	SubscribeLogs() chan string
	UnsubscribeLogs(ch chan string)
	Shutdown(ctx context.Context)
}

type Telemetry struct {
	lp       *log.LoggerProvider
	mp       *metric.MeterProvider
	tp       *trace.TracerProvider
	log      *slog.Logger
	meter    otelmetric.Meter
	tracer   oteltrace.Tracer
	streamer *LogStreamer
	cfg      Config
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

	streamer := NewLogStreamer()
	sHandler := newStreamerHandler(streamer)

	logger := slog.New(
		slog.NewMultiHandler(jsonHandler, otelHandler, sHandler),
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
		lp:       lp,
		mp:       mp,
		tp:       tp,
		log:      logger,
		meter:    meter,
		tracer:   tracer,
		streamer: streamer,
		cfg:      cfg,
	}, nil
}

func (t *Telemetry) GetServiceName() string {
	return t.cfg.ServiceName
}

func (t *Telemetry) GetConfig() Config {
	return t.cfg
}

func (t *Telemetry) LogInfo(ctx context.Context, msg string, args ...any) {
	t.log.InfoContext(ctx, msg, args...)
}

func (t *Telemetry) LogErrorln(ctx context.Context, msg string, args ...any) {
	t.log.ErrorContext(ctx, msg, args...)
}

func (t *Telemetry) LogFatalln(ctx context.Context, msg string, args ...any) {
	t.log.ErrorContext(ctx, msg, args...)
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

func (t *Telemetry) SubscribeLogs() chan string {
	if t.streamer != nil {
		return t.streamer.Subscribe()
	}
	ch := make(chan string)
	close(ch)
	return ch
}

func (t *Telemetry) UnsubscribeLogs(ch chan string) {
	if t.streamer != nil {
		t.streamer.Unsubscribe(ch)
	}
}

func (t *Telemetry) Shutdown(ctx context.Context) {
	t.lp.Shutdown(ctx)
	t.mp.Shutdown(ctx)
	t.tp.Shutdown(ctx)
}

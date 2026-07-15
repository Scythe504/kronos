package nodes

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"

	"github.com/scythe504/kronos/internal/telemetry"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/mem"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// StartSystemStatsPublisher registers OpenTelemetry observable gauges
// to collect real-time system stats (CPU, Memory, GPU) periodically using callbacks.
func StartSystemStatsPublisher(ctx context.Context, nodeID string) error {
	meter := otel.Meter("kronos-nodes")
	attrs := metric.WithAttributes(attribute.String("node_id", nodeID))

	// 1. CPU Utilization Gauge
	_, err := meter.Float64ObservableGauge(
		telemetry.MetricNodeCPUUtilization.Name,
		metric.WithDescription(telemetry.MetricNodeCPUUtilization.Description),
		metric.WithUnit(telemetry.MetricNodeCPUUtilization.Unit),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			percentages, err := cpu.PercentWithContext(ctx, 0, false)
			if err == nil && len(percentages) > 0 {
				obs.Observe(percentages[0], attrs)
			} else {
				slog.ErrorContext(ctx, "Failed to collect CPU utilization metric", slog.Any("error", err))
			}
			return nil
		}),
	)
	if err != nil {
		return err
	}

	// 2. Memory Used Gauge
	_, err = meter.Int64ObservableGauge(
		telemetry.MetricNodeMemoryUsed.Name,
		metric.WithDescription(telemetry.MetricNodeMemoryUsed.Description),
		metric.WithUnit(telemetry.MetricNodeMemoryUsed.Unit),
		metric.WithInt64Callback(func(ctx context.Context, obs metric.Int64Observer) error {
			vmem, err := mem.VirtualMemoryWithContext(ctx)
			if err == nil {
				obs.Observe(int64(vmem.Used), attrs)
			} else {
				slog.ErrorContext(ctx, "Failed to collect memory utilization metric", slog.Any("error", err))
			}
			return nil
		}),
	)
	if err != nil {
		return err
	}

	// 3. GPU Utilization Gauge
	_, err = meter.Float64ObservableGauge(
		telemetry.MetricNodeGPUUtilization.Name,
		metric.WithDescription(telemetry.MetricNodeGPUUtilization.Description),
		metric.WithUnit(telemetry.MetricNodeGPUUtilization.Unit),
		metric.WithFloat64Callback(func(ctx context.Context, obs metric.Float64Observer) error {
			nvidiaSmiPath, err := exec.LookPath("nvidia-smi")
			if err != nil {
				// No Nvidia GPU present
				obs.Observe(0.0, attrs)
				return nil
			}
			cmd := exec.CommandContext(ctx, nvidiaSmiPath, "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits")
			var out bytes.Buffer
			cmd.Stdout = &out
			if err := cmd.Run(); err == nil {
				lines := strings.Split(strings.TrimSpace(out.String()), "\n")
				if len(lines) > 0 {
					valStr := strings.TrimSpace(lines[0])
					if val, err := strconv.ParseFloat(valStr, 64); err == nil {
						obs.Observe(val, attrs)
						return nil
					}
				}
			}
			obs.Observe(0.0, attrs)
			return nil
		}),
	)
	return err
}

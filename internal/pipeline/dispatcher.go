package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/nodes"
	"github.com/scythe504/kronos/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

var tracer = otel.Tracer("kronos-pipeline")

func (p *Pipeline) Start(ctx context.Context) {
	nodeCfg := nodes.GetNodeConfig(ctx)
	meter := otel.Meter("kronos-pipeline")
	tasksPulledCounter, _ := meter.Int64Counter(
		telemetry.MetricTasksPulled.Name,
		metric.WithDescription(telemetry.MetricTasksPulled.Description),
		metric.WithUnit(telemetry.MetricTasksPulled.Unit),
	)

	pollCount := 1

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var tasks []database.Task
		var err error

		func() {
			pollCtx, span := tracer.Start(ctx, "PollTasks")
			defer span.End()
			span.SetAttributes(
				attribute.String("node_id", p.nodeID),
				attribute.String("task_unit", string(nodeCfg.TaskUnit)),
			)

			tasks, err = p.db.GetTasks(pollCtx, p.nodeID, nodeCfg.TaskUnit)
			if err != nil {
				slog.ErrorContext(pollCtx, "Failed to poll tasks from database", slog.Any("error", err))
				return
			}

			if len(tasks) > 0 {
				slog.InfoContext(pollCtx, "Successfully leased tasks from queue", slog.Int("count", len(tasks)))
				tasksPulledCounter.Add(pollCtx, int64(len(tasks)), metric.WithAttributes(
					attribute.String("node_id", p.nodeID),
					attribute.String("task_unit", string(nodeCfg.TaskUnit)),
				))
			}
		}()

		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		if len(tasks) == 0 {
			retryCount := min(5, pollCount)
			timeDuration := JitterTime(retryCount).Seconds()
			time.Sleep(time.Duration(timeDuration))
			continue
		}

		pollCount = 1
		for _, task := range tasks {
			task := task
			go func() {
				taskCtx, span := tracer.Start(ctx, fmt.Sprintf("ExecuteTask:%s", task.PayloadSlug))
				defer span.End()
				span.SetAttributes(
					attribute.String("task_id", task.ID.String()),
					attribute.String("task_slug", task.PayloadSlug),
					attribute.String("task_unit", string(task.AllocatedUnit)),
				)

				adapted, err := AdaptTask(task)
				if err != nil {
					slog.ErrorContext(taskCtx, "Failed to adapt task payload", slog.String("task_id", task.ID.String()), slog.Any("error", err))
					return
				}

				p.AddInFlightTask(task.ID, taskCtx)
				if err := p.Enqueue(taskCtx, task.PayloadSlug, adapted); err != nil {
					slog.ErrorContext(taskCtx, "Failed to enqueue task to worker pipeline", slog.String("task_id", task.ID.String()), slog.Any("error", err))
					p.RemoveInFlightTask(task.ID)
				}
			}()
		}
	}
}

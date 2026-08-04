package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/nodes"
	"github.com/scythe504/kronos/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (p *Pipeline) Start(ctx context.Context) {
	nodeCfg := nodes.GetNodeConfig(ctx)
	tasksPulledCounter, err := p.tel.MeterInt64Counter(telemetry.MetricTasksPulled)
	if err != nil {
		p.tel.LogFatalln(ctx, "Failed to initialize tasks pulled counter", "error", err.Error())
	}

	pollCount := 1

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var tasks []database.Task

		func() {
			pollCtx, span := p.tel.TraceStart(ctx, "PollTasks")
			defer span.End()
			span.SetAttributes(
				attribute.String("node_id", p.nodeID),
				attribute.String("task_unit", string(nodeCfg.TaskUnit)),
			)

			tasks, err = p.db.GetTasks(pollCtx, p.nodeID, []database.TaskUnit{nodeCfg.TaskUnit}, p.allowedSlugs)
			if err != nil {
				p.tel.LogErrorln(pollCtx, "Failed to poll tasks from database", "error", err.Error())
				return
			}

			if len(tasks) > 0 {
				p.tel.LogInfo(pollCtx, "Successfully leased tasks from queue", "count", len(tasks))
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

			if p.taskQueueDurationHist != nil && !task.CreatedAt.IsZero() {
				queueDurationMs := time.Since(task.CreatedAt).Milliseconds()
				p.taskQueueDurationHist.Record(ctx, queueDurationMs, metric.WithAttributes(
					attribute.String("task_slug", task.PayloadSlug),
					attribute.String("node_id", p.nodeID),
				))
			}

			go func() {
				taskCtx, span := p.tel.TraceStart(ctx, fmt.Sprintf("ExecuteTask:%s", task.PayloadSlug))
				defer span.End()
				span.SetAttributes(
					attribute.String("task_id", task.ID.String()),
					attribute.String("task_slug", task.PayloadSlug),
					attribute.String("task_unit", string(task.AllocatedUnit)),
				)

				adapted, err := AdaptTask(task)
				if err != nil {
					p.tel.LogErrorln(taskCtx, "Failed to adapt task payload", "task_id", task.ID.String(), "error", err.Error())
					return
				}

				p.AddInFlightTask(task.ID)
				if err := p.Enqueue(taskCtx, task.PayloadSlug, adapted); err != nil {
					p.tel.LogErrorln(taskCtx, "Failed to enqueue task to worker pipeline", "task_id", task.ID.String(), "error", err.Error())
					p.RemoveInFlightTask(task.ID)
				}
			}()
		}
	}
}

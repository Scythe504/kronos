package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (p *Pipeline) ObserveProcessStdout(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		p.tel.LogErrorln(ctx, "Failed to get stdout pipe", "slug", slug, "error", err.Error())
		return
	}
	scanner := bufio.NewScanner(pipe.Stdout)
	for scanner.Scan() {
		go p.ResultHandler(ctx, json.RawMessage(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		p.tel.LogErrorln(ctx, "Error scanning stdout", "slug", slug, "error", err.Error())
		return
	}
}

func (p *Pipeline) ObserveProcessStderr(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		p.tel.LogErrorln(ctx, "Failed to get stderr pipe", "slug", slug, "error", err.Error())
		return
	}
	scanner := bufio.NewScanner(pipe.Stderr)
	for scanner.Scan() {
		p.tel.LogErrorln(ctx, "Worker Stderr output", "slug", slug, "output", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		p.tel.LogErrorln(ctx, "Error scanning stderr", "slug", slug, "error", err.Error())
		return
	}
}

// handles success, ack and error messages back from worker process to route them to update tasks table
func (p *Pipeline) ResultHandler(ctx context.Context, rawRes json.RawMessage) {
	var wr WorkerResult
	if err := json.Unmarshal(rawRes, &wr); err != nil {
		p.tel.LogErrorln(ctx, "Failed to unmarshal worker result payload", "payload", string(rawRes), "error", err.Error())
		return
	}
	wr.Timestamp = time.Now()

	taskMeta, ok := p.GetInFlightTask(wr.TaskID)
	var taskCtx context.Context
	if !ok {
		taskCtx = ctx
	} else {
		taskCtx = taskMeta.Ctx
		if p.taskExecutionDurationHist != nil && !taskMeta.StartTime.IsZero() {
			execDurationMs := time.Since(taskMeta.StartTime).Milliseconds()
			p.taskExecutionDurationHist.Record(taskCtx, execDurationMs, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		if wr.ResultMessage != WorkerResultACKMessage {
			p.RemoveInFlightTask(wr.TaskID)
		}
	}

	switch wr.ResultMessage {
	case WorkerResultSuccessMesssage:
		p.tel.LogInfo(taskCtx, "Task execution succeeded", "task_id", wr.TaskID.String())
		p.db.CompleteTask(taskCtx, wr.TaskID, wr.Timestamp, wr.Output)
	case WorkerResultFailedMessage:
		p.tel.LogErrorln(taskCtx, "Task execution failed", "task_id", wr.TaskID.String(), "error", string(wr.Error))
		if p.tasksFailedCounter != nil {
			p.tasksFailedCounter.Add(taskCtx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		_, _, err := p.db.FailTask(taskCtx, wr.TaskID, wr.Error, wr.Timestamp)
		if err == nil && p.taskRetriesCounter != nil {
			p.taskRetriesCounter.Add(taskCtx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
	case WorkerResultACKTimeoutMessage:
		p.tel.LogErrorln(taskCtx, "Task execution timed out (ACK timeout)", "task_id", wr.TaskID.String())
		if p.tasksFailedCounter != nil {
			p.tasksFailedCounter.Add(taskCtx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		p.db.FailTask(taskCtx, wr.TaskID, []byte(`{"error": "worker process failed to acknowledge tasks"}`), wr.Timestamp)
	case WorkerResultACKMessage:
		p.tel.LogInfo(taskCtx, "Task execution acknowledged by worker", "task_id", wr.TaskID.String())
		return
	}
}

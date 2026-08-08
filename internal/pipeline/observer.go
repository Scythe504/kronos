package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
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
		text := strings.TrimSpace(scanner.Text())
		if text == "" {
			continue
		}
		if strings.HasPrefix(text, "{") && strings.HasSuffix(text, "}") {
			go p.ResultHandler(ctx, json.RawMessage(text))
		} else {
			p.tel.LogInfo(ctx, "Worker Stdout log", "slug", slug, "output", text)
		}
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
		p.tel.LogInfo(ctx, "Worker Stderr output", "slug", slug, "output", scanner.Text())
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

	startTime, ok := p.GetInFlightTask(wr.TaskID)
	if ok {
		if p.taskExecutionDurationHist != nil && !startTime.IsZero() {
			execDurationMs := time.Since(startTime).Milliseconds()
			p.taskExecutionDurationHist.Record(ctx, execDurationMs, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		if wr.ResultMessage != WorkerResultACKMessage {
			p.RemoveInFlightTask(wr.TaskID)
		}
	}

	switch wr.ResultMessage {
	case WorkerResultSuccessMesssage:
		p.tel.LogInfo(ctx, "Task execution succeeded", "task_id", wr.TaskID.String())
		p.db.CompleteTask(ctx, wr.TaskID, wr.Timestamp, wr.Output)
	case WorkerResultFailedMessage:
		p.tel.LogErrorln(ctx, "Task execution failed", "task_id", wr.TaskID.String(), "error", string(wr.Error))
		if p.tasksFailedCounter != nil {
			p.tasksFailedCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		_, _, err := p.db.FailTask(ctx, wr.TaskID, wr.Error, wr.Timestamp)
		if err == nil && p.taskRetriesCounter != nil {
			p.taskRetriesCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
	case WorkerResultACKTimeoutMessage:
		p.tel.LogErrorln(ctx, "Task execution timed out (ACK timeout)", "task_id", wr.TaskID.String())
		if p.tasksFailedCounter != nil {
			p.tasksFailedCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("task_id", wr.TaskID.String()),
			))
		}
		p.db.FailTask(ctx, wr.TaskID, []byte(`{"error": "worker process failed to acknowledge tasks"}`), wr.Timestamp)
	case WorkerResultACKMessage:
		p.tel.LogInfo(ctx, "Task execution acknowledged by worker", "task_id", wr.TaskID.String())
		return
	}
}

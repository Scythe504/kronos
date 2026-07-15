package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

func (p *Pipeline) ObserveProcessStdout(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get stdout pipe", slog.String("slug", slug), slog.Any("error", err))
		return
	}
	scanner := bufio.NewScanner(pipe.Stdout)
	for scanner.Scan() {
		go p.ResultHandler(ctx, json.RawMessage(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		slog.ErrorContext(ctx, "Error scanning stdout", slog.String("slug", slug), slog.Any("error", err))
		return
	}
}

func (p *Pipeline) ObserveProcessStderr(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get stderr pipe", slog.String("slug", slug), slog.Any("error", err))
		return
	}
	scanner := bufio.NewScanner(pipe.Stderr)
	for scanner.Scan() {
		slog.ErrorContext(ctx, "Worker Stderr output", slog.String("slug", slug), slog.String("output", scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		slog.ErrorContext(ctx, "Error scanning stderr", slog.String("slug", slug), slog.Any("error", err))
		return
	}
}

// handles success, ack and error messages back from worker process to route them to update tasks table
func (p *Pipeline) ResultHandler(ctx context.Context, rawRes json.RawMessage) {
	var wr WorkerResult
	if err := json.Unmarshal(rawRes, &wr); err != nil {
		slog.ErrorContext(ctx, "Failed to unmarshal worker result payload", slog.String("payload", string(rawRes)), slog.Any("error", err))
		return
	}
	wr.Timestamp = time.Now()

	taskCtx, ok := p.GetInFlightTask(wr.TaskID)
	if !ok {
		taskCtx = ctx
	} else {
		if wr.ResultMessage != WorkerResultACKMessage {
			p.RemoveInFlightTask(wr.TaskID)
		}
	}

	switch wr.ResultMessage {
	case WorkerResultSuccessMesssage:
		slog.InfoContext(taskCtx, "Task execution succeeded", slog.String("task_id", wr.TaskID.String()))
		p.db.CompleteTask(taskCtx, wr.TaskID, wr.Timestamp, wr.Output)
	case WorkerResultFailedMessage:
		slog.ErrorContext(taskCtx, "Task execution failed", slog.String("task_id", wr.TaskID.String()), slog.String("error", string(wr.Error)))
		p.db.FailTask(taskCtx, wr.TaskID, wr.Error, wr.Timestamp)
	case WorkerResultACKTimeoutMessage:
		slog.ErrorContext(taskCtx, "Task execution timed out (ACK timeout)", slog.String("task_id", wr.TaskID.String()))
		p.db.FailTask(taskCtx, wr.TaskID, []byte(`{"error": "worker process failed to acknowledge tasks"}`), wr.Timestamp)
	case WorkerResultACKMessage:
		slog.InfoContext(taskCtx, "Task execution acknowledged by worker", slog.String("task_id", wr.TaskID.String()))
		return
	}
}

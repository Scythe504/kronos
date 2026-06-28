package pipeline

import (
	"bufio"
	"context"
	"encoding/json"
	"log"
	"time"
)

func (p *Pipeline) ObserveProcessStdout(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
	scanner := bufio.NewScanner(pipe.Stdout)
	for scanner.Scan() {
		go p.ResultHandler(ctx, json.RawMessage(scanner.Text()))
	}
	if err := scanner.Err(); err != nil {
		log.Println("ERR(ObserveProcessStdout):", err)
		return
	}
}

func (p *Pipeline) ObserveProcessStderr(ctx context.Context, slug string) {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		log.Println("ERR(ObserveProcessStderr):", err)
		return
	}
	scanner := bufio.NewScanner(pipe.Stderr)
	for scanner.Scan() {
		log.Println("ERR(ScanningStderr)", scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		log.Println("ERR(ObserveProcessStderr):", err)
		return
	}
}

// handles success, ack and error messages back from worker process to route them to update tasks table
func (p *Pipeline) ResultHandler(ctx context.Context, rawRes json.RawMessage) {
	var wr WorkerResult
	if err := json.Unmarshal(rawRes, &wr); err != nil {
		log.Println("ERR(UnmarshalingWorkerResult)", err, string(rawRes))
		return
	}
	wr.Timestamp = time.Now()

	switch wr.ResultMessage {
	case WorkerResultSuccessMesssage:
		p.db.CompleteTask(ctx, wr.TaskID, wr.Timestamp)
	case WorkerResultFailedMessage:
		p.db.FailTask(ctx, wr.TaskID, wr.Error, wr.Timestamp)
	case WorkerResultACKTimeoutMessage:
		p.db.FailTask(ctx, wr.TaskID, []byte(`{"error": "worker process failed to acknowledge tasks"}`), wr.Timestamp)
	case WorkerResultACKMessage:
		return
	}
}

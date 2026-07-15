package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"

	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

var pipelineTracer = otel.Tracer("kronos-pipeline")

type Pipeline struct {
	db            database.Service
	nodeID        string
	registry      Registry
	inFlightMu    sync.RWMutex
	inFlightTasks map[uuid.UUID]context.Context
}

func Init(db database.Service, nodeID string) *Pipeline {
	pipeline := &Pipeline{
		db:     db,
		nodeID: nodeID,
		registry: Registry{
			processes: make(map[string]*Pipe),
			mu:        sync.RWMutex{},
		},
		inFlightTasks: make(map[uuid.UUID]context.Context),
	}

	return pipeline
}

func (p *Pipeline) AddInFlightTask(id uuid.UUID, ctx context.Context) {
	p.inFlightMu.Lock()
	defer p.inFlightMu.Unlock()
	p.inFlightTasks[id] = ctx
}

func (p *Pipeline) GetInFlightTask(id uuid.UUID) (context.Context, bool) {
	p.inFlightMu.RLock()
	defer p.inFlightMu.RUnlock()
	ctx, ok := p.inFlightTasks[id]
	return ctx, ok
}

func (p *Pipeline) RemoveInFlightTask(id uuid.UUID) {
	p.inFlightMu.Lock()
	defer p.inFlightMu.Unlock()
	delete(p.inFlightTasks, id)
}

type Pipe struct {
	Stdin   io.WriteCloser
	Stdout  io.ReadCloser
	Stderr  io.ReadCloser
	writeMu sync.Mutex
}

type Registry struct {
	processes map[string]*Pipe
	mu        sync.RWMutex
}

func (p *Pipeline) GetPipe(ctx context.Context, slug string) (*Pipe, error) {
	p.registry.mu.RLock()
	pipe, ok := p.registry.processes[slug]
	p.registry.mu.RUnlock()
	if ok {
		return pipe, nil
	}

	p.registry.mu.Lock()
	defer p.registry.mu.Unlock()

	// Double-check if another thread started it while acquiring lock
	if pipe, ok = p.registry.processes[slug]; ok {
		return pipe, nil
	}

	// Start the process synchronously under the lock
	pipe, err := p.StartWorkerProcess(ctx, slug)
	if err != nil {
		return nil, err
	}

	p.registry.processes[slug] = pipe
	return pipe, nil
}

func (p *Pipeline) StartWorkerProcess(ctx context.Context, slug string) (*Pipe, error) {
	ctx, span := pipelineTracer.Start(ctx, "StartWorkerProcess")
	defer span.End()
	span.SetAttributes(attribute.String("slug", slug))

	slog.InfoContext(ctx, "Spawning worker process", slog.String("slug", slug))

	cmdPath := ""
	switch slug {
	case "csv-pdf", "csv_to_pdf":
		cmdPath = "examples/csv-pdf/main.go"
	case "transcoder":
		cmdPath = "examples/transcoder/main.go"
	default:
		return nil, fmt.Errorf("unknown task slug: %s", slug)
	}

	cmd := exec.Command("go", "run", cmdPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	go p.ObserveProcessStdout(ctx, slug)
	go p.ObserveProcessStderr(ctx, slug)

	// Asynchronously wait to reap the process status and prevent zombie processes
	go func() {
		err := cmd.Wait()
		p.registry.mu.Lock()
		delete(p.registry.processes, slug)
		p.registry.mu.Unlock()

		if err != nil {
			slog.ErrorContext(ctx, "Worker process exited with error", slog.String("slug", slug), slog.Any("error", err))
		} else {
			slog.InfoContext(ctx, "Worker process exited successfully", slog.String("slug", slug))
		}
	}()

	return &Pipe{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}, nil
}

func (p *Pipeline) Enqueue(ctx context.Context, slug string, payload []byte) error {
	ctx, span := pipelineTracer.Start(ctx, "EnqueueTask")
	defer span.End()

	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		return fmt.Errorf("no worker process found for slug: %s", slug)
	}

	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		traceparent := fmt.Sprintf("00-%s-%s-%s", spanCtx.TraceID(), spanCtx.SpanID(), spanCtx.TraceFlags())
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err == nil {
			data["traceparent"] = traceparent
			if updatedPayload, err := json.Marshal(data); err == nil {
				payload = updatedPayload
			}
		}
	}

	payload = append(payload, '\n')
	pipe.writeMu.Lock()
	defer pipe.writeMu.Unlock()
	_, err = pipe.Stdin.Write(payload)

	return err
}

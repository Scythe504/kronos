package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type TaskMeta struct {
	Ctx       context.Context
	StartTime time.Time
}

type Pipeline struct {
	db                        database.Service
	nodeID                    string
	tel                       telemetry.TelemetryProvider
	registry                  Registry
	inFlightMu                sync.RWMutex
	inFlightTasks             map[uuid.UUID]TaskMeta
	tasksFailedCounter        otelmetric.Int64Counter
	taskRetriesCounter        otelmetric.Int64Counter
	activeWorkersCounter      otelmetric.Int64UpDownCounter
	taskExecutionDurationHist otelmetric.Int64Histogram
	taskQueueDurationHist     otelmetric.Int64Histogram
	workerSpawnDurationHist   otelmetric.Int64Histogram
}

func Init(db database.Service, nodeID string, tel telemetry.TelemetryProvider) *Pipeline {
	tasksFailedCounter, _ := tel.MeterInt64Counter(telemetry.MetricTasksFailed)
	taskRetriesCounter, _ := tel.MeterInt64Counter(telemetry.MetricTaskRetries)
	activeWorkersCounter, _ := tel.MeterInt64UpDownCounter(telemetry.MetricActiveWorkers)
	taskExecutionDurationHist, _ := tel.MeterInt64Histogram(telemetry.MetricTaskExecutionDuration)
	taskQueueDurationHist, _ := tel.MeterInt64Histogram(telemetry.MetricTaskQueueDuration)
	workerSpawnDurationHist, _ := tel.MeterInt64Histogram(telemetry.MetricWorkerSpawnDuration)

	pipeline := &Pipeline{
		db:                        db,
		nodeID:                    nodeID,
		tel:                       tel,
		tasksFailedCounter:        tasksFailedCounter,
		taskRetriesCounter:        taskRetriesCounter,
		activeWorkersCounter:      activeWorkersCounter,
		taskExecutionDurationHist: taskExecutionDurationHist,
		taskQueueDurationHist:     taskQueueDurationHist,
		workerSpawnDurationHist:   workerSpawnDurationHist,
		registry: Registry{
			processes: make(map[string]*Pipe),
			mu:        sync.RWMutex{},
		},
		inFlightTasks: make(map[uuid.UUID]TaskMeta),
	}

	return pipeline
}

func (p *Pipeline) AddInFlightTask(id uuid.UUID, ctx context.Context) {
	p.inFlightMu.Lock()
	defer p.inFlightMu.Unlock()
	p.inFlightTasks[id] = TaskMeta{
		Ctx:       ctx,
		StartTime: time.Now(),
	}
}

func (p *Pipeline) GetInFlightTask(id uuid.UUID) (TaskMeta, bool) {
	p.inFlightMu.RLock()
	defer p.inFlightMu.RUnlock()
	meta, ok := p.inFlightTasks[id]
	return meta, ok
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
	spawnStartTime := time.Now()
	ctx, span := p.tel.TraceStart(ctx, "StartWorkerProcess")
	defer span.End()
	span.SetAttributes(attribute.String("slug", slug))

	p.tel.LogInfo(ctx, "Spawning worker process", "slug", slug)

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

	spawnDurationMs := time.Since(spawnStartTime).Milliseconds()
	if p.workerSpawnDurationHist != nil {
		p.workerSpawnDurationHist.Record(ctx, spawnDurationMs, otelmetric.WithAttributes(
			attribute.String("slug", slug),
			attribute.String("node_id", p.nodeID),
		))
	}

	if p.activeWorkersCounter != nil {
		p.activeWorkersCounter.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("slug", slug),
			attribute.String("node_id", p.nodeID),
		))
	}

	go p.ObserveProcessStdout(ctx, slug)
	go p.ObserveProcessStderr(ctx, slug)

	// Asynchronously wait to reap the process status and prevent zombie processes
	go func() {
		err := cmd.Wait()
		p.registry.mu.Lock()
		delete(p.registry.processes, slug)
		p.registry.mu.Unlock()

		if p.activeWorkersCounter != nil {
			p.activeWorkersCounter.Add(ctx, -1, otelmetric.WithAttributes(
				attribute.String("slug", slug),
				attribute.String("node_id", p.nodeID),
			))
		}

		if err != nil {
			p.tel.LogErrorln(ctx, "Worker process exited with error", "slug", slug, "error", err.Error())
		} else {
			p.tel.LogInfo(ctx, "Worker process exited successfully", "slug", slug)
		}
	}()

	return &Pipe{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}, nil
}

func (p *Pipeline) Enqueue(ctx context.Context, slug string, payload []byte) error {
	ctx, span := p.tel.TraceStart(ctx, "EnqueueTask")
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

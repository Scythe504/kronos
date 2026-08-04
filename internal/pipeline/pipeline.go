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
	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

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

type Pipeline struct {
	db                        database.Service
	nodeID                    string
	tel                       telemetry.TelemetryProvider
	builder                   *builder.Manager
	registry                  Registry
	workerCacheMu             sync.RWMutex
	workerCache               map[string]database.Worker
	inFlightMu                sync.RWMutex
	inFlightTasks             map[uuid.UUID]time.Time
	tasksFailedCounter        otelmetric.Int64Counter
	taskRetriesCounter        otelmetric.Int64Counter
	activeWorkersCounter      otelmetric.Int64UpDownCounter
	taskExecutionDurationHist otelmetric.Int64Histogram
	taskQueueDurationHist     otelmetric.Int64Histogram
	workerSpawnDurationHist   otelmetric.Int64Histogram
	allowedSlugs              []string
}

func Init(db database.Service, nodeID string, tel telemetry.TelemetryProvider, bm *builder.Manager, allowedSlugs []string) *Pipeline {
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
		builder:                   bm,
		allowedSlugs:              allowedSlugs,
		tasksFailedCounter:        tasksFailedCounter,
		taskRetriesCounter:        taskRetriesCounter,
		activeWorkersCounter:      activeWorkersCounter,
		taskExecutionDurationHist: taskExecutionDurationHist,
		taskQueueDurationHist:     taskQueueDurationHist,
		workerSpawnDurationHist:   workerSpawnDurationHist,
		workerCache:               make(map[string]database.Worker),
		inFlightTasks:             make(map[uuid.UUID]time.Time),
		registry: Registry{
			processes: make(map[string]*Pipe),
			mu:        sync.RWMutex{},
		},
	}

	return pipeline
}

func (p *Pipeline) AddInFlightTask(id uuid.UUID) {
	p.inFlightMu.Lock()
	defer p.inFlightMu.Unlock()
	p.inFlightTasks[id] = time.Now()
}

func (p *Pipeline) GetInFlightTask(id uuid.UUID) (time.Time, bool) {
	p.inFlightMu.RLock()
	defer p.inFlightMu.RUnlock()
	t, ok := p.inFlightTasks[id]
	return t, ok
}

func (p *Pipeline) RemoveInFlightTask(id uuid.UUID) {
	p.inFlightMu.Lock()
	defer p.inFlightMu.Unlock()
	delete(p.inFlightTasks, id)
}

// GetWorker retrieves worker metadata from the in-memory cache, falling back to database query.
func (p *Pipeline) GetWorker(ctx context.Context, slug string) (database.Worker, error) {
	p.workerCacheMu.RLock()
	worker, ok := p.workerCache[slug]
	p.workerCacheMu.RUnlock()
	if ok {
		return worker, nil
	}

	p.workerCacheMu.Lock()
	defer p.workerCacheMu.Unlock()

	if worker, ok = p.workerCache[slug]; ok {
		return worker, nil
	}

	w, err := p.db.GetWorker(ctx, slug)
	if err != nil {
		return database.Worker{}, fmt.Errorf("failed fetching worker metadata for slug %s: %w", slug, err)
	}

	p.workerCache[slug] = w
	return w, nil
}

// PrecacheWorkers populates the in-memory cache and triggers background container compilation for allowed_slugs.
func (p *Pipeline) PrecacheWorkers(ctx context.Context, allowedSlugs []string) {
	for _, slug := range allowedSlugs {
		worker, err := p.GetWorker(ctx, slug)
		if err != nil {
			p.tel.LogErrorln(ctx, "Failed precaching worker", "slug", slug, "error", err)
			continue
		}

		if p.builder != nil {
			res, err := p.builder.Build(ctx, worker)
			if err != nil {
				p.tel.LogErrorln(ctx, "Failed pre-building worker image", "slug", slug, "error", err)
			} else {
				p.tel.LogInfo(ctx, "Pre-built worker image ready", "slug", slug, "image", res.ImageTag, "cached", res.Cached)
			}
		}
	}
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

	if pipe, ok = p.registry.processes[slug]; ok {
		return pipe, nil
	}

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

	worker, err := p.GetWorker(ctx, slug)
	if err != nil {
		return nil, fmt.Errorf("unknown task worker slug '%s': %w", slug, err)
	}

	var cmd *exec.Cmd
	if p.builder != nil {
		buildRes, err := p.builder.Build(ctx, worker)
		if err != nil {
			return nil, fmt.Errorf("failed building container image for worker '%s': %w", slug, err)
		}
		p.tel.LogInfo(ctx, "Executing worker container process", "slug", slug, "image", buildRes.ImageTag, "cached", buildRes.Cached)
		cmd = exec.CommandContext(ctx, "docker", "run", "--rm", "-i", buildRes.ImageTag)
	} else {
		cmd = exec.CommandContext(ctx, "go", "run", fmt.Sprintf("examples/%s/main.go", slug))
	}

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

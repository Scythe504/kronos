package pipeline

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"

	"github.com/scythe504/fluxd/internal/database"
)

type Pipeline struct {
	db       database.Service
	registry Registry
}

func Init(ctx context.Context) *Pipeline {
	pipeline := &Pipeline{
		db: database.New(ctx),
		registry: Registry{
			processes: make(map[string]*Pipe),
			mu:        sync.RWMutex{},
		},
	}

	return pipeline
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
	// 1. Fast path: Read Lock
	p.registry.mu.RLock()
	pipe, ok := p.registry.processes[slug]
	p.registry.mu.RUnlock()
	if ok {
		return pipe, nil
	}

	// 2. Slow path: Write Lock
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
	cmdPath := ""
	switch slug {
	case "video_transcode":
		cmdPath = "./examples/transcoder/main.go"
	case "csv_to_pdf":
		cmdPath = "./examples/csv-pdf/main.go"
	default:
		return nil, fmt.Errorf("unhandled worker slug: %s", slug)
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
		_ = cmd.Wait()
		p.registry.mu.Lock()
		delete(p.registry.processes, slug)
		p.registry.mu.Unlock()
	}()

	return &Pipe{
		Stdin:  stdin,
		Stdout: stdout,
		Stderr: stderr,
	}, nil
}

func (p *Pipeline) Enqueue(ctx context.Context, slug string, payload []byte) error {
	pipe, err := p.GetPipe(ctx, slug)
	if err != nil {
		return fmt.Errorf("no worker process found for slug: %s", slug)
	}

	pipe.writeMu.Lock()
	defer pipe.writeMu.Unlock()
	_, err = pipe.Stdin.Write(payload)

	return err
}

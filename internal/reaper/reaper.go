package reaper

import (
	"context"
	"time"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
)

type Config struct {
	Interval           time.Duration
	NodeDeadThreshold  time.Duration
	TaskStuckThreshold time.Duration
}

type Reaper struct {
	db     database.Service
	tel    telemetry.TelemetryProvider
	config Config
}

func New(db database.Service, tel telemetry.TelemetryProvider, cfg Config) *Reaper {
	if cfg.Interval <= 0 {
		cfg.Interval = 30 * time.Second
	}
	if cfg.NodeDeadThreshold <= 0 {
		cfg.NodeDeadThreshold = 2 * time.Minute
	}
	if cfg.TaskStuckThreshold <= 0 {
		cfg.TaskStuckThreshold = 10 * time.Minute
	}

	return &Reaper{
		db:     db,
		tel:    tel,
		config: cfg,
	}
}

func (r *Reaper) Start(ctx context.Context) {
	r.tel.LogInfo(ctx, "Starting Reaper background process",
		"interval", r.config.Interval.String(),
		"node_threshold", r.config.NodeDeadThreshold.String(),
		"task_threshold", r.config.TaskStuckThreshold.String(),
	)

	ticker := time.NewTicker(r.config.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.tel.LogInfo(ctx, "Stopping Reaper background process")
			return
		case <-ticker.C:
			r.reap(ctx)
		}
	}
}

func (r *Reaper) reap(ctx context.Context) {
	reapedNodes, err := r.db.ReapDeadNodes(ctx, r.config.NodeDeadThreshold)
	if err != nil {
		r.tel.LogErrorln(ctx, "Reaper failed marking dead nodes", "error", err)
	} else if reapedNodes > 0 {
		r.tel.LogInfo(ctx, "Reaper marked stale nodes as dead", "count", reapedNodes)
	}

	reapedTasks, err := r.db.ReapStuckTasks(ctx, r.config.TaskStuckThreshold)
	if err != nil {
		r.tel.LogErrorln(ctx, "Reaper failed requeuing stuck tasks", "error", err)
	} else if reapedTasks > 0 {
		r.tel.LogInfo(ctx, "Reaper requeued stuck running tasks", "count", reapedTasks)
	}
}

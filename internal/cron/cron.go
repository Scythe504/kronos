package cron

import (
	"context"
	"time"

	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/telemetry"
)

type Scheduler struct {
	db      database.Service
	tel     telemetry.TelemetryProvider
	builder *builder.Manager
}

func NewScheduler(db database.Service, tel telemetry.TelemetryProvider, bm *builder.Manager) *Scheduler {
	return &Scheduler{
		db:      db,
		tel:     tel,
		builder: bm,
	}
}

func (s *Scheduler) Start(ctx context.Context) {
	s.checkAndTrigger(ctx)

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkAndTrigger(ctx)
		}
	}
}

func (s *Scheduler) checkAndTrigger(ctx context.Context) {
	runIDs, err := s.db.TriggerDueCronWorkflows(ctx)
	if err != nil {
		if s.tel != nil {
			s.tel.LogErrorln(ctx, "Failed to trigger due cron workflows", "error", err)
		}
		return
	}

	if len(runIDs) > 0 {
		if s.tel != nil {
			s.tel.LogInfo(ctx, "Triggered due cron workflow runs", "count", len(runIDs))
		}
	}
}

package cron

import (
	"context"
	"log"
	"time"

	"github.com/scythe504/kronos/internal/database"
)

type Scheduler struct {
	db database.Service
}

func NewScheduler(db database.Service) *Scheduler {
	return &Scheduler{
		db: db,
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
		log.Println("ERR(CronScheduler): failed to trigger due cron workflows: ", err)
		return
	}

	if len(runIDs) > 0 {
		log.Printf("INFO(CronScheduler): triggered %d workflow run(s)\n", len(runIDs))
	}
}

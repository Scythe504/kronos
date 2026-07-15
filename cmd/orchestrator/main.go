package main

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/cron"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/nodes"
	"github.com/scythe504/kronos/internal/pipeline"
	"github.com/scythe504/kronos/internal/telemetry"
)

func main() {
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize OpenTelemetry
	telCfg, err := telemetry.NewConfigFromEnv()
	if err != nil {
		log.Fatal("[ERR_TELEMETRY_CFG_FAIL]:", err)
	}
	tel, err := telemetry.NewTelemetry(ctx, telCfg)
	if err != nil {
		log.Fatal("[ERR_TELEMETRY_INIT_FAIL]:", err)
	}
	defer tel.Shutdown(ctx)

	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()
	db := database.New(dbCtx)

	nodeCfg := nodes.GetNodeConfig(ctx)
	nodeCfg.TaskUnit = database.TaskUnitCPU

	// Attempt to read previously registered node ID from local file
	if data, err := os.ReadFile(".node_id"); err == nil {
		if parsed, err := uuid.Parse(strings.TrimSpace(string(data))); err == nil {
			nodeCfg.ID = &parsed
		}
	}

	id, err := db.RegisterNode(dbCtx, *nodeCfg)
	if err != nil {
		log.Fatal("[ERR_DAEMON_REG_FAIL]:", err)
	}

	// Persist the assigned unique ID locally
	_ = os.WriteFile(".node_id", []byte(id), 0644)

	// Start publishing node resource metrics (CPU/Memory/GPU)
	if err := nodes.StartSystemStatsPublisher(ctx, id); err != nil {
		log.Println("[WARN_TELEMETRY_STATS_FAIL]:", err)
	}

	p := pipeline.Init(db, id)

	cronSched := cron.NewScheduler(db)
	wg.Go(func() { cronSched.Start(ctx) })

	wg.Go(func() { nodes.SendHeartbeat(db, ctx, id) })

	wg.Go(func() { p.Start(ctx) })

	wg.Wait()
	// give in-flight tasks time to finish
	log.Println("Orchestrator stopped")
}

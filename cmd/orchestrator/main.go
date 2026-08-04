package main

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/joho/godotenv/autoload"
	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/cron"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/nodes"
	"github.com/scythe504/kronos/internal/pipeline"
	"github.com/scythe504/kronos/internal/telemetry"
	"github.com/scythe504/kronos/internal/utils"
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
	// Initialize telemetry with fallback
	var tel telemetry.TelemetryProvider
	tel, err = telemetry.NewTelemetry(ctx, telCfg)
	if err != nil {
		log.Println("[WARN] Failed to create telemetry, falling back to no-op telemetry:", err)
		tel, _ = telemetry.NewNoopTelemetry(telCfg)
	}
	defer tel.Shutdown(ctx)

	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()
	db := database.New(dbCtx)

	nodeCfg := nodes.GetNodeConfig(ctx)
	nodeCfg.TaskUnit = database.TaskUnitCPU

	nodeIDPath := utils.GetNodeIDFilePath()
	if data, err := os.ReadFile(nodeIDPath); err == nil {
		if parsed, err := uuid.Parse(strings.TrimSpace(string(data))); err == nil {
			nodeCfg.ID = &parsed
		}
	}

	id, err := db.RegisterNode(dbCtx, *nodeCfg)
	if err != nil {
		log.Fatal("[ERR_DAEMON_REG_FAIL]:", err)
	}

	// Persist the assigned unique ID locally
	_ = os.MkdirAll(filepath.Dir(nodeIDPath), 0755)
	_ = os.WriteFile(nodeIDPath, []byte(id), 0644)

	// Start publishing node resource metrics (CPU/Memory/GPU)
	if err := nodes.StartSystemStatsPublisher(ctx, id); err != nil {
		log.Println("[WARN_TELEMETRY_STATS_FAIL]:", err)
	}

	// Initialize Builder Manager
	builderMgr, err := builder.NewManager(builder.NewDefaultConfig(), tel)
	if err != nil {
		log.Println("[WARN] Failed to initialize builder manager:", err)
	}

	allowedSlugsStr := os.Getenv("ALLOWED_SLUGS")
	var allowedSlugs []string
	if allowedSlugsStr != "" {
		for _, s := range strings.Split(allowedSlugsStr, ",") {
			if trimmed := strings.TrimSpace(s); trimmed != "" {
				allowedSlugs = append(allowedSlugs, trimmed)
			}
		}
	}

	p := pipeline.Init(db, id, tel, builderMgr, allowedSlugs)
	p.PrecacheWorkers(ctx, allowedSlugs)

	cronSched := cron.NewScheduler(db, tel, builderMgr)
	wg.Go(func() { cronSched.Start(ctx) })

	wg.Go(func() {
		nodes.SendHeartbeat(db, ctx, id, nodeCfg.TaskUnit, func(hbCtx context.Context, newSlugs []string) {
			p.SyncAllowedSlugs(hbCtx, newSlugs)
		})
	})

	wg.Go(func() { p.Start(ctx) })

	wg.Wait()
	log.Println("Orchestrator stopped")
}

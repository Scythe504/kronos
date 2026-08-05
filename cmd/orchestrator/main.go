package main

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/cron"
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

	var tel telemetry.TelemetryProvider
	tel, err = telemetry.NewTelemetry(ctx, telCfg)
	if err != nil {
		log.Println("[WARN] Failed to create telemetry, falling back to no-op telemetry:", err)
		tel, _ = telemetry.NewNoopTelemetry(telCfg)
	}
	defer tel.Shutdown(ctx)

	// Load configuration from OS-native agent.conf if available
	nodes.LoadAgentConfig()

	dbURL := os.Getenv("DB_URL")
	masterURL := os.Getenv("MASTER_URL")

	nodeCfg := nodes.InitNodeConfig(ctx)

	db, id, err := nodes.RegisterOrInitNode(ctx, nodeCfg, dbURL, masterURL)
	if err != nil {
		log.Fatal("[ERR_NODE_REGISTRATION_FAIL]:", err)
	}

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

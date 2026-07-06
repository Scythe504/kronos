package main

import (
	"context"
	"log"
	"sync"
	"time"

	_ "github.com/joho/godotenv/autoload"
	"github.com/scythe504/kronos/internal/cron"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/nodes"
	"github.com/scythe504/kronos/internal/pipeline"
)

func main() {
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()
	db := database.New(dbCtx)

	nodeCfg := nodes.GetNodeConfig(ctx)
	nodeCfg.TaskUnit = database.TaskUnitCPU

	id, err := db.RegisterNode(dbCtx, *nodeCfg)
	if err != nil {
		log.Fatal("[ERR_DAEMON_REG_FAIL]:", err)
	}
	p := pipeline.Init(db)

	cronSched := cron.NewScheduler(db)
	wg.Go(func() { cronSched.Start(ctx) })

	wg.Go(func() { nodes.SendHeartbeat(db, ctx, id) })

	wg.Go(func() { p.Start(ctx) })

	wg.Wait()
	// give in-flight tasks time to finish
	log.Println("Orchestrator stopped")
}

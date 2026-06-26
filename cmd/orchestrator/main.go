package main

import (
	"context"
	"log"
	"sync"

	_ "github.com/joho/godotenv/autoload"
	"github.com/scythe504/fluxd/internal/pipeline"
)

func main() {
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pipeline.Init(ctx)

	for range 5 {
		wg.Go(func() {
			p.Start(ctx)
		})
	}

	wg.Wait()
	// give in-flight tasks time to finish
	log.Println("Orchestrator stopped")
}

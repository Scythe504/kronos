package main

import (
	"context"
	"log"
	
	_ "github.com/joho/godotenv/autoload"
	"github.com/scythe504/fluxd/internal/pipeline"
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := pipeline.Init(ctx)

	p.Start(ctx)

	// give in-flight tasks time to finish
	log.Println("Orchestrator stopped")
}

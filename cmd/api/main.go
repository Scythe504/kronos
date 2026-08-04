package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/scythe504/kronos/internal/server"
	"github.com/scythe504/kronos/internal/telemetry"
)

func gracefulShutdown(apiServer *http.Server, done chan bool, ctx context.Context, stop context.CancelFunc, tel telemetry.TelemetryProvider) {
	// Wait for the initial termination signal
	<-ctx.Done()

	log.Println("Shutting down telemetry...")
	tel.Shutdown(context.Background())

	log.Println("Shutting down gracefully, press Ctrl+C again to force quit...")

	forceChan := make(chan os.Signal, 1)
	signal.Notify(forceChan, syscall.SIGINT, syscall.SIGTERM)

	cx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdownErr := make(chan error, 1)
	go func() {
		shutdownErr <- apiServer.Shutdown(cx)
	}()

	select {
	case err := <-shutdownErr:
		if err != nil {
			log.Printf("Server forced to shutdown with error: %v\n", err)
		} else {
			log.Println("Server connections cleared successfully.")
		}
	case <-forceChan:
		log.Println("Force quit signal received. Exiting immediately.")
	}

	// Clean up NotifyContext resources
	stop()
	log.Println("Server exiting")
	done <- true
}

func main() {
	// Initialize context that listens for the initial termination signal
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel() // Ensure resources are cleaned up if main exits unexpectedly

	// Load telemetry config
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

	server := server.New(ctx, tel)
	done := make(chan bool, 1)

	// Pass context to the graceful shutdown monitor
	go gracefulShutdown(server, done, ctx, cancel, tel)

	// Execution blocks here under normal circumstances
	err = server.ListenAndServe()
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
		panic(fmt.Sprintf("http server error: %s", err))
	}

	// Block until gracefulShutdown signals completion
	<-done
	log.Println("Graceful shutdown complete.")
}

package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/scythe504/kronos/internal/cron"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/reaper"
	"github.com/scythe504/kronos/internal/telemetry"
)

type Server struct {
	db  database.Service
	tel telemetry.TelemetryProvider
}

func NewServer(db database.Service, tel telemetry.TelemetryProvider) *Server {
	return &Server{
		db:  db,
		tel: tel,
	}
}

func New(ctx context.Context, db database.Service, tel telemetry.TelemetryProvider) *http.Server {
	srv := NewServer(db, tel)

	r := reaper.New(srv.db, tel, reaper.Config{})
	go r.Start(ctx)

	cronSched := cron.NewScheduler(srv.db, tel)
	go cronSched.Start(ctx)

	port := 8080
	if p := os.Getenv("PORT"); p != "" {
		if val, err := strconv.Atoi(p); err == nil && val > 0 {
			port = val
		}
	}

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      srv.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return httpServer
}

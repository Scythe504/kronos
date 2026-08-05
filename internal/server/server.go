package server

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/reaper"
	"github.com/scythe504/kronos/internal/telemetry"
)

type Server struct {
	db   database.Service
	tel  telemetry.TelemetryProvider
	port int
}

func NewServer(db database.Service, tel telemetry.TelemetryProvider) *Server {
	port, _ := strconv.Atoi(os.Getenv("PORT"))
	return &Server{
		port: port,
		db:   db,
		tel:  tel,
	}
}

func New(ctx context.Context, tel telemetry.TelemetryProvider) *http.Server {
	srv := NewServer(database.New(ctx, ""), tel)

	r := reaper.New(srv.db, tel, reaper.Config{})
	go r.Start(ctx)

	httpServer := &http.Server{
		Addr:         fmt.Sprintf(":%d", srv.port),
		Handler:      srv.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return httpServer
}

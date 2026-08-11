package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/scythe504/kronos/internal/telemetry"
	"github.com/scythe504/kronos/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func (s *Server) RegisterRoutes() http.Handler {
	m := chi.NewMux()
	m.Use(s.corsMiddleware)
	m.Use(s.telemetryMiddleware)
	m.Get("/health", s.healthHandler)
	m.Get("/setup.sh", s.installScriptHandler)
	m.Get("/setup.ps1", s.installPS1Handler)

	// api/v1 routes
	m.Route("/api/v1", func(r chi.Router) {
		// Worker routes
		r.Post("/workers", s.createWorker)
		r.Get("/workers", s.getWorkers)
		r.Get("/workers/{slug}", s.getWorker)
		r.Put("/workers/{slug}", s.updateWorker)
		r.Delete("/workers/{slug}", s.deleteWorker)

		// Node routes
		r.Post("/nodes/init", s.initNode)
		r.Get("/nodes", s.getNodes)
		r.Get("/nodes/{id}", s.getNode)
		r.Put("/nodes/{id}", s.updateNode)

		// Task routes
		r.Post("/tasks", s.createTask)
		r.Post("/tasks/bulk", s.createTasksBulk)
		r.Get("/tasks", s.getTasks)
		r.Get("/tasks/stats", s.getTaskStats)
		r.Get("/tasks/{id}", s.getTaskByID)
		r.Post("/tasks/retry", s.retryTasks)
		r.Delete("/tasks/{id}", s.deleteTask)

		// Workflow routes
		r.Post("/workflows/templates", s.createWorkflowTemplate)
		r.Get("/workflows/templates", s.getWorkflowTemplates)
		r.Get("/workflows/templates/{id}", s.getWorkflowTemplate)
		r.Put("/workflows/templates/{id}", s.updateWorkflowTemplate)
		r.Delete("/workflows/templates/{id}", s.deleteWorkflowTemplate)
		r.Post("/workflows/{id}/trigger", s.triggerWorkflow)
		r.Post("/workflows/task-chain", s.createTaskchain)
		r.Post("/webhooks/{workflow_id}", s.triggerWorkflow)

		// SSE Log & Metrics Streaming
		r.Get("/metrics/stream", s.streamLogs)
		r.Get("/logs/stream", s.streamLogs)

		// Secure Prometheus Read-Only Proxy
		r.Get("/metrics/prometheus/query", s.queryPrometheusProxy)
		r.Get("/metrics/prometheus/query_range", s.queryPrometheusRangeProxy)
	})

	return m
}

// Telemetry middleware for logging and tracing HTTP requests
func (s *Server) telemetryMiddleware(next http.Handler) http.Handler {
	inFlightCounter, _ := s.tel.MeterInt64UpDownCounter(telemetry.MetricRequestsInFlight)
	durationHistogram, _ := s.tel.MeterInt64Histogram(telemetry.MetricRequestDurationMillis)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		if inFlightCounter != nil {
			inFlightCounter.Add(ctx, 1, metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
			))
			defer inFlightCounter.Add(ctx, -1, metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
			))
		}

		// Start trace span for request
		ctx, span := s.tel.TraceStart(ctx, fmt.Sprintf("HTTP %s %s", r.Method, r.URL.Path))
		defer span.End()

		s.tel.LogInfo(ctx, "HTTP request started", "method", r.Method, "path", r.URL.Path)

		startTime := time.Now()

		next.ServeHTTP(w, r.WithContext(ctx))

		durationMs := time.Since(startTime).Milliseconds()
		if durationHistogram != nil {
			durationHistogram.Record(ctx, durationMs, metric.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.path", r.URL.Path),
			))
		}

		s.tel.LogInfo(ctx, "HTTP request completed",
			"method", r.Method,
			"path", r.URL.Path,
			"duration_ms", durationMs,
		)
	})
}

// CORS middleware
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// CORS Headers
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS, PATCH")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Credentials", "true")

		// Handle preflight OPTIONS requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthHandler(w http.ResponseWriter, _ *http.Request) {
	resp := s.db.Health()

	utils.WriteJSON(w, http.StatusOK, resp)
}

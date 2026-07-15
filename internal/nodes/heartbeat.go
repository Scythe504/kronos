package nodes

import (
	"context"
	"log/slog"
	"time"

	"github.com/scythe504/kronos/internal/database"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("kronos-nodes")

// Starts a Heartbeat Loop with interval time of 5 seconds
func SendHeartbeat(db database.Service, ctx context.Context, machineId string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			func() {
				hbeatCtx, span := tracer.Start(ctx, "Heartbeat")
				defer span.End()
				span.SetAttributes(attribute.String("node_id", machineId))

				if _, err := db.UpdateNodeLastHBeat(hbeatCtx, machineId); err != nil {
					slog.ErrorContext(hbeatCtx, "Failed to send node heartbeat", slog.String("node_id", machineId), slog.Any("error", err))
					return
				}
				slog.DebugContext(hbeatCtx, "Node heartbeat sent successfully", slog.String("node_id", machineId))
			}()
		case <-ctx.Done():
			return
		}
	}
}

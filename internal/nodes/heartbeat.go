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

// SendHeartbeat runs a heartbeat loop that updates the node's last_heartbeat timestamp
// and re-derives the allowed slug list from registered workers matching the node's task_unit.
// When the slug list changes, onSlugsChanged is called so the pipeline can evict
// stale images and pre-cache newly assigned slugs.
func SendHeartbeat(db database.Service, ctx context.Context, nodeID string, taskUnit database.TaskUnit, onSlugsChanged func(ctx context.Context, newSlugs []string)) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	var currentSlugs []string

	for {
		select {
		case <-ticker.C:
			func() {
				hbeatCtx, span := tracer.Start(ctx, "Heartbeat")
				defer span.End()
				span.SetAttributes(attribute.String("node_id", nodeID))

				if _, err := db.UpdateNodeLastHBeat(hbeatCtx, nodeID); err != nil {
					slog.ErrorContext(hbeatCtx, "Failed to send node heartbeat", slog.String("node_id", nodeID), slog.Any("error", err))
					return
				}
				slog.DebugContext(hbeatCtx, "Node heartbeat sent successfully", slog.String("node_id", nodeID))

				if onSlugsChanged == nil {
					return
				}

				workers, err := db.GetWorkers(hbeatCtx, 1, 200)
				if err != nil {
					return
				}

				var newSlugs []string
				for _, w := range workers {
					if w.TaskUnit == taskUnit {
						newSlugs = append(newSlugs, w.Slug)
					}
				}

				if !slugsEqual(currentSlugs, newSlugs) {
					slog.InfoContext(hbeatCtx, "Allowed slugs changed, syncing pipeline", slog.String("node_id", nodeID))
					onSlugsChanged(hbeatCtx, newSlugs)
					currentSlugs = newSlugs
				}
			}()
		case <-ctx.Done():
			return
		}
	}
}

// slugsEqual checks whether two slug slices contain the same elements regardless of order.
func slugsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, s := range a {
		counts[s]++
	}
	for _, s := range b {
		counts[s]--
		if counts[s] < 0 {
			return false
		}
	}
	return true
}

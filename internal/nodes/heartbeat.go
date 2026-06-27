package nodes

import (
	"context"
	"time"

	"github.com/scythe504/kronos/internal/database"
)

// Starts a Heartbeat Loop with interval time of 5 seconds
func SendHeartbeat(db database.Service, ctx context.Context, machineId string) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if _, err := db.UpdateNodeLastHBeat(ctx, machineId); err != nil {
				continue
			}
		case <-ctx.Done():
			return
		}
	}
}

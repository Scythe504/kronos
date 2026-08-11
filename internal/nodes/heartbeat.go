package nodes

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

var tracer = otel.Tracer("kronos-nodes")

// SendHeartbeat runs a heartbeat loop that updates the node's last_heartbeat timestamp
// and re-derives the allowed slug list from registered workers matching the node's task_unit.
// When the slug list changes, onSlugsChanged is called so the pipeline can evict
// stale images and pre-cache newly assigned slugs.
func SendHeartbeat(db database.Service, ctx context.Context, nodeID string, nodeCfg *database.Node, onSlugsChanged func(ctx context.Context, newSlugs []string)) {
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

				// Query own node info to sync any changes from dashboard (e.g. task_unit or allowed_slugs)
				nodeInfo, err := db.GetNode(hbeatCtx, nodeID)
				if err != nil {
					slog.ErrorContext(hbeatCtx, "Failed to get node details in heartbeat", slog.String("node_id", nodeID), slog.Any("error", err))
					return
				}

				// Sync database's task_unit back to our daemon's running config
				if nodeInfo.TaskUnit != "" {
					nodeCfg.TaskUnit = nodeInfo.TaskUnit
				}

				var newSlugs []string
				if len(nodeInfo.AllowedSlugs) > 0 {
					newSlugs = nodeInfo.AllowedSlugs
				} else {
					workers, err := db.GetWorkers(hbeatCtx, 1, 200)
					if err != nil {
						return
					}

					// Split task unit by comma, pipe, or space (e.g. "cpu,gpu" or "cpu|gpu")
					var units []string
					for _, part := range strings.FieldsFunc(string(nodeInfo.TaskUnit), func(r rune) bool {
						return r == ',' || r == '|' || r == '&' || r == ' '
					}) {
						trimmed := strings.TrimSpace(part)
						if trimmed != "" {
							units = append(units, trimmed)
						}
					}

					for _, w := range workers {
						// Match worker unit against node units
						matched := false
						for _, u := range units {
							if string(w.TaskUnit) == u {
								matched = true
								break
							}
						}
						if matched {
							newSlugs = append(newSlugs, w.Slug)
						}
					}
				}

				// If there's a difference in allowed slugs or task unit, save to agent.conf
				if !slugsEqual(currentSlugs, newSlugs) || string(nodeInfo.TaskUnit) != os.Getenv("TASK_UNIT") || strings.Join(newSlugs, ",") != os.Getenv("ALLOWED_SLUGS") {
					// Update local config file agent.conf
					agentConfPath := utils.GetAgentConfigFilePath()
					envMap := make(map[string]string)
					if _, err := os.Stat(agentConfPath); err == nil {
						if readMap, err := godotenv.Read(agentConfPath); err == nil {
							envMap = readMap
						}
					}
					envMap["TASK_UNIT"] = string(nodeInfo.TaskUnit)
					envMap["ALLOWED_SLUGS"] = strings.Join(newSlugs, ",")
					if err := godotenv.Write(envMap, agentConfPath); err != nil {
						slog.ErrorContext(hbeatCtx, "Failed to update agent.conf on sync", slog.Any("error", err))
					} else {
						// Update the environment variables of the running process too
						os.Setenv("TASK_UNIT", string(nodeInfo.TaskUnit))
						os.Setenv("ALLOWED_SLUGS", strings.Join(newSlugs, ","))
						slog.InfoContext(hbeatCtx, "Saved updated configuration to agent.conf", slog.String("task_unit", string(nodeInfo.TaskUnit)), slog.String("allowed_slugs", strings.Join(newSlugs, ",")))
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

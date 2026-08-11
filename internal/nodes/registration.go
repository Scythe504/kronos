package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
)

// InitNodeConfig resolves TaskUnit and existing node_id file from disk.
func InitNodeConfig(ctx context.Context) *database.Node {
	nodeCfg := GetNodeConfig(ctx)
	if envUnit := os.Getenv("TASK_UNIT"); envUnit != "" {
		nodeCfg.TaskUnit = database.TaskUnit(envUnit)
	} else {
		nodeCfg.TaskUnit = database.TaskUnitCPU
	}

	nodeIDPath := utils.GetNodeIDFilePath()
	if data, err := os.ReadFile(nodeIDPath); err == nil {
		if parsed, err := uuid.Parse(strings.TrimSpace(string(data))); err == nil {
			nodeCfg.ID = &parsed
		}
	}
	return nodeCfg
}

// RegisterOrInitNode registers node hardware specs via direct DB or Master HTTP API, returning database.Service and node ID.
func RegisterOrInitNode(ctx context.Context, nodeCfg *database.Node, dbURL, masterURL string) (database.Service, string, error) {
	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()

	var db database.Service
	var id string

	// Registration is strictly HTTP-based (routed through the master server)
	effectiveMasterURL := masterURL
	if effectiveMasterURL == "" {
		effectiveMasterURL = "http://localhost:8080"
	}

	reqBytes, err := json.Marshal(nodeCfg)
	if err != nil {
		return nil, "", fmt.Errorf("master node init marshal failed: %w", err)
	}

	resp, err := http.Post(strings.TrimRight(effectiveMasterURL, "/")+"/api/v1/nodes/init", "application/json", bytes.NewReader(reqBytes))
	if err != nil {
		return nil, "", fmt.Errorf("master node init http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("master returned status %d", resp.StatusCode)
	}

	var initResp struct {
		NodeID       string   `json:"node_id"`
		DBURL        string   `json:"db_url"`
		AllowedSlugs []string `json:"allowed_slugs"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&initResp); err != nil {
		return nil, "", fmt.Errorf("master node init response decode failed: %w", err)
	}

	id = initResp.NodeID

	effectiveDBURL := dbURL
	if effectiveDBURL == "" {
		effectiveDBURL = initResp.DBURL
	}
	if effectiveDBURL != "" {
		db = database.New(dbCtx, effectiveDBURL)
	}

	nodeIDPath := utils.GetNodeIDFilePath()
	if err := os.MkdirAll(filepath.Dir(nodeIDPath), 0755); err == nil {
		_ = os.WriteFile(nodeIDPath, []byte(id), 0644)
	}

	return db, id, nil
}

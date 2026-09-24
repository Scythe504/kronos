package nodes

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/server"
	"github.com/scythe504/kronos/internal/telemetry"
	"github.com/scythe504/kronos/internal/testutil"
	"github.com/scythe504/kronos/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupNodeTestServer sets up an isolated database and an httptest.Server running Kronos Master API.
func setupNodeTestServer(t *testing.T) (database.Service, *httptest.Server, context.Context) {
	t.Helper()
	testutil.IsolateConfigDir(t)
	ResetNodeConfigForTesting()

	ctx := context.Background()
	dbURL := testutil.GetTestDBURL(t)
	t.Setenv("DB_URL", dbURL)
	t.Setenv("SECRET_KEY", "test-secret-key-for-node-tests")

	dbService := database.New(ctx, dbURL)

	noopTel, err := telemetry.NewNoopTelemetry(telemetry.Config{ServiceName: "kronos-test-master"})
	require.NoError(t, err)

	srv := server.NewServer(dbService, noopTel)
	ts := httptest.NewServer(srv.RegisterRoutes())
	t.Cleanup(ts.Close)

	return dbService, ts, ctx
}

func TestRegisterOrInitNode_FreshRegistration(t *testing.T) {
	dbService, ts, ctx := setupNodeTestServer(t)

	// Seed a test worker so allowed_slugs can be populated
	_, err := dbService.UpsertWorker(ctx, nil, []database.Worker{
		{
			Slug:       "cpu-test-worker",
			Name:       "CPU Test Worker",
			RepoURL:    "https://github.com/test/repo",
			RepoRef:    "main",
			Entrypoint: "./worker",
			TaskUnit:   database.TaskUnitCPU,
		},
	})
	require.NoError(t, err)

	machineID := "test-machine-" + uuid.New().String()
	nodeCfg := &database.Node{
		MachineID:    machineID,
		Kernel:       "Linux 6.8.0",
		Architecture: "amd64",
		CPUModel:     "AMD Ryzen 9",
		CPUCores:     16,
		RAMKB:        32768,
		IPAddr:       "10.0.0.42",
		Hostname:     "node-host-1",
		TaskUnit:     database.TaskUnitCPU,
		NodeVersion:  "v1.0.0",
	}

	returnedDB, nodeID, err := RegisterOrInitNode(ctx, nodeCfg, ts.URL)
	require.NoError(t, err)
	require.NotEmpty(t, nodeID)
	_, err = uuid.Parse(nodeID)
	require.NoError(t, err, "expected nodeID to be a valid UUID")

	// Verify returned DB connection is functional
	require.NotNil(t, returnedDB)
	health := returnedDB.Health()
	assert.Equal(t, "up", health["status"])

	// Verify node_id was written to the isolated config directory
	nodeIDFilePath := utils.GetNodeIDFilePath()
	data, err := os.ReadFile(nodeIDFilePath)
	require.NoError(t, err)
	assert.Equal(t, nodeID, strings.TrimSpace(string(data)))

	// Verify the node record is properly stored in Postgres
	dbNode, err := dbService.GetNode(ctx, nodeID)
	require.NoError(t, err)
	assert.Equal(t, nodeID, dbNode.ID.String())
	assert.Equal(t, machineID, dbNode.MachineID)
	assert.Equal(t, database.NodeStatusIdle, dbNode.Status)
	assert.Equal(t, "node-host-1", dbNode.Hostname)
	assert.Equal(t, database.TaskUnitCPU, dbNode.TaskUnit)
}

func TestRegisterOrInitNode_ReInit_Idempotency(t *testing.T) {
	dbService, ts, ctx := setupNodeTestServer(t)

	machineID := "reinit-machine-" + uuid.New().String()
	initialNode := &database.Node{
		MachineID:    machineID,
		Kernel:       "Linux 6.8.0",
		Architecture: "amd64",
		CPUModel:     "Intel Xeon",
		CPUCores:     8,
		RAMKB:        16384,
		IPAddr:       "10.0.0.10",
		Hostname:     "reinit-host",
		TaskUnit:     database.TaskUnitCPU,
		NodeVersion:  "v1.0.0",
	}

	// 1. Initial registration
	_, firstID, err := RegisterOrInitNode(ctx, initialNode, ts.URL)
	require.NoError(t, err)
	require.NotEmpty(t, firstID)

	// 2. Simulate node restart: InitNodeConfig reads persisted .node_id from disk
	reloadedCfg := InitNodeConfig(ctx)
	require.NotNil(t, reloadedCfg.ID)
	assert.Equal(t, firstID, reloadedCfg.ID.String())

	// Update some hardware specs to verify update-on-conflict
	reloadedCfg.MachineID = machineID
	reloadedCfg.Hostname = "reinit-host-updated"
	reloadedCfg.RAMKB = 65536

	// 3. Re-register
	_, secondID, err := RegisterOrInitNode(ctx, reloadedCfg, ts.URL)
	require.NoError(t, err)
	assert.Equal(t, firstID, secondID, "node ID should remain unchanged across re-initialization")

	// 4. Verify no duplicate records were created in Postgres
	nodes, err := dbService.GetNodes(ctx, 1, 100)
	require.NoError(t, err)
	assert.Len(t, nodes, 1, "should have exactly 1 node record after re-initialization")

	// Verify specs were updated
	dbNode, err := dbService.GetNode(ctx, firstID)
	require.NoError(t, err)
	assert.Equal(t, "reinit-host-updated", dbNode.Hostname)
	assert.Equal(t, int64(65536), dbNode.RAMKB)
}

func TestRegisterOrInitNode_InactiveNodeRejected(t *testing.T) {
	dbService, ts, ctx := setupNodeTestServer(t)

	nodeCfg := &database.Node{
		MachineID:    "inactive-test-machine",
		Kernel:       "Linux 6.8.0",
		Architecture: "amd64",
		CPUModel:     "Intel i7",
		CPUCores:     8,
		RAMKB:        16384,
		IPAddr:       "10.0.0.99",
		Hostname:     "inactive-node",
		TaskUnit:     database.TaskUnitCPU,
		NodeVersion:  "v1.0.0",
	}

	_, nodeID, err := RegisterOrInitNode(ctx, nodeCfg, ts.URL)
	require.NoError(t, err)

	// Mark node as inactive
	_, err = dbService.UpdateNodeStatus(ctx, nodeID, database.NodeStatusInactive)
	require.NoError(t, err)

	// Re-attempt registration with the inactive node ID
	parsedID := uuid.MustParse(nodeID)
	nodeCfg.ID = &parsedID

	_, _, err = RegisterOrInitNode(ctx, nodeCfg, ts.URL)
	require.Error(t, err, "re-registration of inactive node should fail")
	assert.Contains(t, err.Error(), "master returned status 500")
}

func TestRegisterOrInitNode_UnreachableMaster(t *testing.T) {
	testutil.IsolateConfigDir(t)
	ResetNodeConfigForTesting()

	ctx := context.Background()
	nodeCfg := &database.Node{
		MachineID: "unreachable-test-node",
		Hostname:  "unreachable-node",
		TaskUnit:  database.TaskUnitCPU,
	}

	// Use an unassigned port
	_, _, err := RegisterOrInitNode(ctx, nodeCfg, "http://127.0.0.1:59999")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "master node init http request failed")

	// Verify node_id was not written
	_, err = os.ReadFile(utils.GetNodeIDFilePath())
	require.Error(t, err, "node_id file should not exist when registration fails")
}
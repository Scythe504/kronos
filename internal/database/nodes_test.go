package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNodeLifecycle(t *testing.T) {
	dbService, _, ctx := setupTestDB(t)

	machineID := "test-node-123"
	gpuVram := int64(8192)
	gpuModel := "NVIDIA RTX 4090"
	node := Node{
		MachineID:     machineID,
		Kernel:        "Linux 6.8.0",
		Architecture:  "amd64",
		GPURamKB:      &gpuVram,
		GPUModel:      &gpuModel,
		CPUModel:      "Intel i9-13900K",
		CPUCores:      24,
		RAMKB:         32768,
		IPAddr:        "192.168.1.50",
		Hostname:      "worker-node-1",
		CloudRegion:   "us-east-1",
		CloudPlatform: "aws",
		TaskUnit:      TaskUnitCPU,
		Status:        NodeStatusActive,
		NodeVersion:   "v1.0.0",
	}

	var nodeID string

	t.Run("Register Node", func(t *testing.T) {
		regID, err := dbService.RegisterNode(ctx, node)
		assert.NoError(t, err)
		assert.NotEmpty(t, regID)
		nodeID = regID

		n, err := dbService.GetNode(ctx, nodeID)
		assert.NoError(t, err)
		assert.Equal(t, nodeID, n.ID.String())
		assert.Equal(t, machineID, n.MachineID)
		assert.Equal(t, NodeStatusIdle, n.Status)
		assert.Equal(t, "Intel i9-13900K", n.CPUModel)
	})

	t.Run("Update Heartbeat", func(t *testing.T) {
		res, err := dbService.UpdateNodeLastHBeat(ctx, nodeID)
		assert.NoError(t, err)
		assert.Equal(t, nodeID, res)

		n, err := dbService.GetNode(ctx, nodeID)
		assert.NoError(t, err)
		assert.NotZero(t, n.LastHeartbeatAt)
	})

	t.Run("Update Status", func(t *testing.T) {
		res, err := dbService.UpdateNodeStatus(ctx, nodeID, NodeStatusInactive)
		assert.NoError(t, err)
		assert.Equal(t, nodeID, res)

		n, err := dbService.GetNode(ctx, nodeID)
		assert.NoError(t, err)
		assert.Equal(t, NodeStatusInactive, n.Status)
	})

	t.Run("Get Nodes Paged list", func(t *testing.T) {
		nodes, err := dbService.GetNodes(ctx, 1, 10)
		assert.NoError(t, err)
		assert.Len(t, nodes, 1)
		assert.Equal(t, nodeID, nodes[0].ID.String())
	})
}

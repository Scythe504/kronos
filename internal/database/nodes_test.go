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

	t.Run("Register Node", func(t *testing.T) {
		regID, err := dbService.RegisterNode(ctx, node)
		assert.NoError(t, err)
		assert.Equal(t, machineID, regID)

		n, err := dbService.GetNode(ctx, machineID)
		assert.NoError(t, err)
		assert.Equal(t, machineID, n.MachineID)
		assert.Equal(t, NodeStatusIdle, n.Status)
		assert.Equal(t, "Intel i9-13900K", n.CPUModel)
	})

	t.Run("Update Heartbeat Transitions to Active", func(t *testing.T) {
		res, err := dbService.UpdateNodeLastHBeat(ctx, machineID)
		assert.NoError(t, err)
		assert.Equal(t, machineID, res)

		n, err := dbService.GetNode(ctx, machineID)
		assert.NoError(t, err)
		assert.NotZero(t, n.LastHeartbeatAt)
		assert.Equal(t, NodeStatusActive, n.Status) // Heartbeat marks node as active
	})

	t.Run("Idempotent Registration", func(t *testing.T) {
		// Register the same node again
		regID, err := dbService.RegisterNode(ctx, node)
		assert.NoError(t, err)
		assert.Equal(t, machineID, regID)

		n, err := dbService.GetNode(ctx, machineID)
		assert.NoError(t, err)
		assert.Equal(t, NodeStatusIdle, n.Status) // Re-registration resets status to idle
	})

	t.Run("Recover Dead Node", func(t *testing.T) {
		// Mark node dead
		_, err := dbService.UpdateNodeStatus(ctx, machineID, NodeStatusDead)
		assert.NoError(t, err)

		n, err := dbService.GetNode(ctx, machineID)
		assert.NoError(t, err)
		assert.Equal(t, NodeStatusDead, n.Status)

		// Re-register node
		regID, err := dbService.RegisterNode(ctx, node)
		assert.NoError(t, err)
		assert.Equal(t, machineID, regID)

		n, err = dbService.GetNode(ctx, machineID)
		assert.NoError(t, err)
		assert.Equal(t, NodeStatusIdle, n.Status) // Resets back to idle!
	})

	t.Run("Re-registration Rejected if Inactive", func(t *testing.T) {
		// Mark node inactive
		_, err := dbService.UpdateNodeStatus(ctx, machineID, NodeStatusInactive)
		assert.NoError(t, err)

		// Trying to register node again should fail
		_, err = dbService.RegisterNode(ctx, node)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "registration rejected: node is marked inactive")
	})

	t.Run("Get Nodes Paged list", func(t *testing.T) {
		nodes, err := dbService.GetNodes(ctx, 1, 10)
		assert.NoError(t, err)
		assert.Len(t, nodes, 1)
		assert.Equal(t, machineID, nodes[0].MachineID)
	})
}

package nodes

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/scythe504/kronos/internal/database"
)

type mockDB struct {
	database.Service
	onHeartbeat func(ctx context.Context, nodeID string) (string, error)
}

func (m *mockDB) UpdateNodeLastHBeat(ctx context.Context, nodeID string) (string, error) {
	if m.onHeartbeat != nil {
		return m.onHeartbeat(ctx, nodeID)
	}
	return nodeID, nil
}

func (m *mockDB) GetWorkers(ctx context.Context, page int, perPage int) ([]database.Worker, error) {
	return nil, nil
}

func (m *mockDB) GetNode(ctx context.Context, nodeID string) (database.Node, error) {
	return database.Node{
		TaskUnit: database.TaskUnitCPU,
		Status:   database.NodeStatusActive,
	}, nil
}

func TestSendHeartbeat(t *testing.T) {
	ctx := t.Context()
	timerCtx, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()

	var heartbeatCount int32
	mock := &mockDB{
		onHeartbeat: func(ctx context.Context, nodeID string) (string, error) {
			atomic.AddInt32(&heartbeatCount, 1)
			cancel()
			return nodeID, nil
		},
	}

	nodeCfg := &database.Node{
		TaskUnit: database.TaskUnitCPU,
	}
	go SendHeartbeat(mock, timerCtx, "dummy-m-id", nodeCfg, nil)
	<-timerCtx.Done()

	count := atomic.LoadInt32(&heartbeatCount)
	if count < 1 {
		t.Errorf("expected at least 1 heartbeat tick, got %d", count)
	}
}

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
	onHeartbeat func(ctx context.Context, machineID string) (string, error)
}

func (m *mockDB) UpdateNodeLastHBeat(ctx context.Context, machineID string) (string, error) {
	if m.onHeartbeat != nil {
		return m.onHeartbeat(ctx, machineID)
	}
	return machineID, nil
}

func TestSendHeartbeat(t *testing.T) {
	ctx := t.Context()
	timerCtx, cancel := context.WithTimeout(ctx, 6 * time.Second)
	defer cancel()

	var heartbeatCount int32
	mock := &mockDB{
		onHeartbeat: func(ctx context.Context, machineID string) (string, error) {
			atomic.AddInt32(&heartbeatCount, 1)
			cancel() // Cancel the timeout context early to speed up the test
			return machineID, nil
		},
	}

	go SendHeartbeat(mock, timerCtx, "dummy-m-id")
	<-timerCtx.Done()

	count := atomic.LoadInt32(&heartbeatCount)
	if count < 1 {
		t.Errorf("expected at least 1 heartbeat tick, got %d", count)
	}
}

package database

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestGetTask(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_task_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	t.Run("Retrieve existing task", func(t *testing.T) {
		taskID, err := dbService.CreateTask(ctx, "test_task_slug", json.RawMessage(`{"data": "test"}`), nil, nil, nil, nil, false)
		assert.NoError(t, err)

		task, err := dbService.GetTask(ctx, taskID.String())
		assert.NoError(t, err)
		assert.Equal(t, taskID, task.ID)
		assert.Equal(t, "test_task_slug", task.PayloadSlug)
		assert.Equal(t, TaskStatusQueued, task.Status)
		assert.JSONEq(t, `{"data": "test"}`, string(task.Payload))
	})

	t.Run("Retrieve non-existent task", func(t *testing.T) {
		nonExistentID := uuid.New().String()
		_, err := dbService.GetTask(ctx, nonExistentID)
		assert.Error(t, err)
	})
}

func TestGetTasks_Concurrency(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_concurrency_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	taskCount := 90
	tasksToCreate := make([]Task, taskCount)
	for i := range taskCount {
		payload, _ := json.Marshal(map[string]any{"index": i})
		tasksToCreate[i] = Task{
			PayloadSlug:   "test_concurrency_slug",
			Payload:       payload,
			Status:        TaskStatusQueued,
			AllocatedUnit: TaskUnitCPU,
		}
	}

	tx, err := s.pool.Begin(ctx)
	assert.NoError(t, err)
	defer tx.Rollback(ctx)

	err = dbService.CreateTasks(ctx, tx, tasksToCreate)
	assert.NoError(t, err)

	err = tx.Commit(ctx)
	assert.NoError(t, err)

	workerCount := 5
	claimedTasks := make([][]Task, workerCount)
	var wg sync.WaitGroup

	for i := range workerCount {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			machineID := uuid.New().String()
			tasks, err := dbService.GetTasks(ctx, machineID, TaskUnitCPU)
			if err != nil {
				t.Errorf("worker %d failed to get tasks: %v", workerIndex, err)
				return
			}
			claimedTasks[workerIndex] = tasks
		}(i)
	}

	wg.Wait()

	seenTasks := make(map[uuid.UUID]int)
	totalClaimed := 0

	for workerIndex, tasks := range claimedTasks {
		totalClaimed += len(tasks)
		for _, task := range tasks {
			if claimingWorker, exists := seenTasks[task.ID]; exists {
				t.Errorf("Duplicate task claim! Task %s claimed by both worker %d and worker %d", task.ID, workerIndex, claimingWorker)
			}
			seenTasks[task.ID] = workerIndex
		}
	}

	assert.Equal(t, taskCount, totalClaimed)
}

func TestGetTasks_Routing(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES 
			('cpu_worker', 'CPU Worker', './cpu', 'cpu'),
			('gpu_worker', 'GPU Worker', './gpu', 'gpu')
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`)
	if err != nil {
		t.Fatalf("failed to seed mock workers: %v", err)
	}

	cpuTaskID, err := dbService.CreateTask(ctx, "cpu_worker", json.RawMessage(`{}`), nil, nil, nil, nil, false)
	assert.NoError(t, err)

	gpuTaskID, err := dbService.CreateTask(ctx, "gpu_worker", json.RawMessage(`{}`), nil, nil, nil, nil, false)
	assert.NoError(t, err)

	cpuNodeTasks, err := dbService.GetTasks(ctx, "cpu-node", TaskUnitCPU)
	assert.NoError(t, err)
	assert.Len(t, cpuNodeTasks, 1)
	assert.Equal(t, cpuTaskID, cpuNodeTasks[0].ID)

	gpuNodeTasks, err := dbService.GetTasks(ctx, "gpu-node", TaskUnitGPU)
	assert.NoError(t, err)
	assert.Len(t, gpuNodeTasks, 1)
	assert.Equal(t, gpuTaskID, gpuNodeTasks[0].ID)
}

func TestCompleteTask(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_task_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	t.Run("Complete one-off task", func(t *testing.T) {
		taskID, err := dbService.CreateTask(ctx, "test_task_slug", json.RawMessage(`{}`), nil, nil, nil, nil, false)
		assert.NoError(t, err)

		completedID, nextID, err := dbService.CompleteTask(ctx, taskID, time.Now(), []byte(`{"result": "ok"}`))
		assert.NoError(t, err)
		assert.Equal(t, taskID, completedID)
		assert.Equal(t, uuid.Nil, nextID)

		task, err := dbService.GetTask(ctx, taskID.String())
		assert.NoError(t, err)
		assert.Equal(t, TaskStatusCompleted, task.Status)
	})

	t.Run("Idempotency: double completion", func(t *testing.T) {
		taskID, err := dbService.CreateTask(ctx, "test_task_slug", json.RawMessage(`{}`), nil, nil, nil, nil, false)
		assert.NoError(t, err)

		_, _, err = dbService.CompleteTask(ctx, taskID, time.Now(), []byte(`{}`))
		assert.NoError(t, err)

		completedID, nextID, err := dbService.CompleteTask(ctx, taskID, time.Now(), []byte(`{}`))
		assert.NoError(t, err)
		assert.Equal(t, taskID, completedID)
		assert.Equal(t, uuid.Nil, nextID)
	})
}

func TestFailTask(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_task_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	t.Run("Fail task increments retry count, sets back to queued, and empties assigned_node_id", func(t *testing.T) {
		taskID, err := dbService.CreateTask(ctx, "test_task_slug", json.RawMessage(`{}`), nil, nil, nil, nil, false)
		assert.NoError(t, err)

		machineID := "node-abc"
		tasks, err := dbService.GetTasks(ctx, machineID, TaskUnitCPU)
		assert.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, taskID, tasks[0].ID)
		assert.Equal(t, &machineID, tasks[0].AssignedNodeID)

		completedID, nextID, err := dbService.FailTask(ctx, taskID, []byte(`{"error": "fail 1"}`), time.Now())
		assert.NoError(t, err)
		assert.Equal(t, taskID, completedID)
		assert.Equal(t, uuid.Nil, nextID)

		task, err := dbService.GetTask(ctx, taskID.String())
		assert.NoError(t, err)
		assert.Equal(t, 1, *task.RetryCount)
		assert.Equal(t, TaskStatusQueued, task.Status)
		assert.Nil(t, task.AssignedNodeID)
		assert.JSONEq(t, `{"error": "fail 1"}`, string(task.LastError))
	})

	t.Run("Fail task fails permanently after max retries", func(t *testing.T) {
		query := `INSERT INTO tasks (payload_slug, payload, status, max_retry_count, allocated_unit) 
			VALUES ($1, $2, 'queued'::task_status, $3, 'cpu'::task_unit) RETURNING id`
		var taskID uuid.UUID
		err := s.pool.QueryRow(ctx, query, "test_task_slug", json.RawMessage(`{}`), 2).Scan(&taskID)
		assert.NoError(t, err)

		_, _, err = dbService.FailTask(ctx, taskID, []byte(`{"err": 1}`), time.Now())
		assert.NoError(t, err)

		completedID, nextID, err := dbService.FailTask(ctx, taskID, []byte(`{"err": 2}`), time.Now())
		assert.NoError(t, err)
		assert.Equal(t, taskID, completedID)
		assert.Equal(t, uuid.Nil, nextID)

		task, err := dbService.GetTask(ctx, taskID.String())
		assert.NoError(t, err)
		assert.Equal(t, 2, *task.RetryCount)
		assert.Equal(t, TaskStatusFailed, task.Status)
	})

	t.Run("Idempotency: double fail task when already failed", func(t *testing.T) {
		query := `INSERT INTO tasks (payload_slug, payload, status, max_retry_count, allocated_unit) 
			VALUES ($1, $2, 'queued'::task_status, $3, 'cpu'::task_unit) RETURNING id`
		var taskID uuid.UUID
		err := s.pool.QueryRow(ctx, query, "test_task_slug", json.RawMessage(`{}`), 1).Scan(&taskID)
		assert.NoError(t, err)

		_, _, err = dbService.FailTask(ctx, taskID, []byte(`{}`), time.Now())
		assert.NoError(t, err)

		completedID, nextID, err := dbService.FailTask(ctx, taskID, []byte(`{}`), time.Now())
		assert.NoError(t, err)
		assert.Equal(t, taskID, completedID)
		assert.Equal(t, uuid.Nil, nextID)
	})
}

func TestCompleteTask_Chain(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_task_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	steps := []Step{
		{
			Slug:             "test_task_slug",
			StepOrder:        1,
			TriggerCondition: TriggerConditionOnSuccess,
			Payload:          json.RawMessage(`{"index": 1}`),
		},
		{
			Slug:             "test_task_slug",
			StepOrder:        2,
			TriggerCondition: TriggerConditionOnSuccess,
			Payload:          json.RawMessage(`{"index": 2}`),
		},
	}

	taskIDs, err := dbService.CreateTaskChain(ctx, steps)
	assert.NoError(t, err)
	assert.Len(t, taskIDs, 2)
	idA, idB := taskIDs[0], taskIDs[1]

	taskA, err := dbService.GetTask(ctx, idA.String())
	assert.NoError(t, err)
	assert.Equal(t, TaskStatusQueued, taskA.Status)
	assert.True(t, taskA.ChainTask)

	taskB, err := dbService.GetTask(ctx, idB.String())
	assert.NoError(t, err)
	assert.Equal(t, TaskStatusPending, taskB.Status)
	assert.True(t, taskB.ChainTask)

	completedID, nextID, err := dbService.CompleteTask(ctx, idA, time.Now(), []byte(`{"data": "chain"}`))
	assert.NoError(t, err)
	assert.Equal(t, idA, completedID)
	assert.Equal(t, idB, nextID)

	taskBUpdate, err := dbService.GetTask(ctx, idB.String())
	assert.NoError(t, err)
	assert.Equal(t, TaskStatusQueued, taskBUpdate.Status)
	assert.JSONEq(t, `{"data": "chain"}`, string(taskBUpdate.Payload))
}

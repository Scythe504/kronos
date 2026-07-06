package database

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *service) GetTask(ctx context.Context, taskId string) (Task, error) {
	query := `SELECT
			id, workflow_run_id, workflow_step_id, workflow_id, payload_slug, payload, retry_count, max_retry_count, 
			last_error, next_retry_at, status, allocated_unit, assigned_node_id, chain_task,
			created_at, updated_at, deleted_at
		FROM tasks
		WHERE id = $1
	`

	rows, err := s.pool.Query(ctx, query, taskId)
	if err != nil {
		return Task{}, err
	}
	defer rows.Close()

	return pgx.CollectOneRow(rows, pgx.RowToStructByName[Task])
}

func (s *service) GetTasks(ctx context.Context, machineID string, taskUnit TaskUnit) ([]Task, error) {
	query := `UPDATE tasks
			SET status = $1, assigned_node_id = $2, updated_at = now()
			WHERE id IN (
				SELECT id
				FROM tasks
				WHERE status = 'queued' AND allocated_unit = $3::task_unit AND (next_retry_at IS NULL OR next_retry_at <= now()) AND deleted_at IS NULL
				ORDER BY created_at ASC
				LIMIT 20
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, workflow_run_id, workflow_step_id, workflow_id, payload_slug, payload, retry_count, max_retry_count, 
				last_error, next_retry_at, status, allocated_unit, assigned_node_id, chain_task,
				created_at, updated_at, deleted_at
		`
	var tasks []Task
	rows, err := s.pool.Query(ctx, query, TaskStatusRunning, machineID, taskUnit)
	if err != nil {
		return tasks, err
	}
	defer rows.Close()

	tasks, err = pgx.CollectRows(rows, pgx.RowToStructByName[Task])
	if err != nil {
		return tasks, err
	}

	return tasks, nil
}

func (s *service) checkWorkflowRunCompletion(ctx context.Context, tx pgx.Tx, runID uuid.UUID) error {
	queryActive := `SELECT COUNT(*) FROM tasks 
		WHERE workflow_run_id = $1 AND status IN ('queued', 'running', 'pending') AND deleted_at IS NULL`
	var activeCount int
	err := tx.QueryRow(ctx, queryActive, runID).Scan(&activeCount)
	if err != nil {
		return err
	}

	if activeCount == 0 {
		queryFailed := `SELECT COUNT(*) FROM tasks 
			WHERE workflow_run_id = $1 AND status = 'failed' AND deleted_at IS NULL`
		var failedCount int
		err = tx.QueryRow(ctx, queryFailed, runID).Scan(&failedCount)
		if err != nil {
			return err
		}

		finalStatus := "success"
		if failedCount > 0 {
			finalStatus = "failed"
		}

		queryUpdateRun := `UPDATE workflow_runs SET status = $1, updated_at = now() WHERE id = $2`
		_, err = tx.Exec(ctx, queryUpdateRun, finalStatus, runID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *service) progressTaskChain(ctx context.Context, tx pgx.Tx, completedTaskID uuid.UUID, condition string, payload any) (uuid.UUID, error) {
	queryChain := `SELECT follow_on_task_id FROM task_chains WHERE trigger_task_id = $1 AND condition = $2`
	rows, err := tx.Query(ctx, queryChain, completedTaskID, condition)
	if err != nil {
		return uuid.Nil, err
	}
	defer rows.Close()

	var followOnIDs []uuid.UUID
	for rows.Next() {
		var childID uuid.UUID
		if err := rows.Scan(&childID); err == nil {
			followOnIDs = append(followOnIDs, childID)
		}
	}

	var lastQueuedID uuid.UUID
	for _, childID := range followOnIDs {
		var queryQueueNextTask string
		var args []any
		if payload != nil {
			queryQueueNextTask = `UPDATE tasks
				SET status = 'queued'::task_status, updated_at = now(), payload = COALESCE($2, payload)
				WHERE id = $1 AND deleted_at IS NULL
				RETURNING id`
			args = []any{childID, payload}
		} else {
			queryQueueNextTask = `UPDATE tasks
				SET status = 'queued'::task_status, updated_at = now()
				WHERE id = $1 AND deleted_at IS NULL
				RETURNING id`
			args = []any{childID}
		}

		var nextTaskID uuid.UUID
		err := tx.QueryRow(ctx, queryQueueNextTask, args...).Scan(&nextTaskID)
		if err == nil {
			lastQueuedID = nextTaskID
		}
	}

	return lastQueuedID, nil
}

func (s *service) progressWorkflowStep(ctx context.Context, tx pgx.Tx, completedTaskID uuid.UUID, workflowRunId, workflowId, workflowStepId *uuid.UUID, condition string, payload any) (uuid.UUID, error) {
	query := fmt.Sprintf(`INSERT INTO tasks (
			workflow_run_id, workflow_id, workflow_step_id, 
			payload_slug, status, allocated_unit, payload
		)
		SELECT $1, $2, next_ws.id, next_ws.slug, $4, w.task_unit, $5
		FROM workers w
		CROSS JOIN workflow_steps current_ws
		INNER JOIN workflow_steps next_ws
			ON current_ws.workflow_id = next_ws.workflow_id
			AND next_ws.step_order = current_ws.step_order + 1
			AND next_ws.condition = '%s'
		WHERE w.slug = next_ws.slug
			AND current_ws.id = $3
			AND current_ws.workflow_id = $2
		RETURNING id
	`, condition)

	args := []any{workflowRunId, workflowId, workflowStepId, TaskStatusQueued, payload}
	var workflowTaskID uuid.UUID
	err := tx.QueryRow(ctx, query, args...).Scan(&workflowTaskID)

	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, err
	}

	if errors.Is(err, pgx.ErrNoRows) {
		if workflowRunId != nil {
			if err := s.checkWorkflowRunCompletion(ctx, tx, *workflowRunId); err != nil {
				return uuid.Nil, err
			}
		}
		return uuid.Nil, nil
	}

	return workflowTaskID, nil
}

func (s *service) CompleteTask(ctx context.Context, id uuid.UUID, timestamp time.Time, outputPayload json.RawMessage) (uuid.UUID, uuid.UUID, error) {
	query := `UPDATE tasks
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status NOT IN ('completed', 'failed') AND deleted_at IS NULL
		RETURNING id, workflow_step_id, workflow_id, workflow_run_id, chain_task
	`
	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadUncommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, query, TaskStatusCompleted, timestamp, id)
	var completedTaskID uuid.UUID
	var workflowStepId, workflowId, workflowRunId *uuid.UUID
	var chainTask bool
	if err := row.Scan(&completedTaskID, &workflowStepId, &workflowId, &workflowRunId, &chainTask); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.Commit(ctx); err != nil {
				return uuid.Nil, uuid.Nil, err
			}
			return id, uuid.Nil, nil
		}
		return uuid.Nil, uuid.Nil, err
	}

	trimmedPayload := bytes.TrimSpace(outputPayload)
	hasOutput := len(trimmedPayload) > 0 && !bytes.Equal(trimmedPayload, []byte("null"))
	var payloadParam any = nil
	if hasOutput {
		payloadParam = outputPayload
	}

	var workflowTaskID uuid.UUID

	if chainTask {
		nextTaskID, err := s.progressTaskChain(ctx, tx, completedTaskID, "on_success", payloadParam)
		if err != nil {
			return completedTaskID, uuid.Nil, err
		}
		if nextTaskID != uuid.Nil {
			workflowTaskID = nextTaskID
		}
	}

	if workflowStepId != nil && workflowId != nil {
		nextTaskID, err := s.progressWorkflowStep(ctx, tx, completedTaskID, workflowRunId, workflowId, workflowStepId, "on_success", payloadParam)
		if err != nil {
			return completedTaskID, uuid.Nil, err
		}
		if nextTaskID == uuid.Nil {
			if err := tx.Commit(ctx); err != nil {
				return uuid.Nil, uuid.Nil, err
			}
			return completedTaskID, uuid.Nil, nil
		}
		workflowTaskID = nextTaskID
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return completedTaskID, workflowTaskID, nil
}

func (s *service) FailTask(ctx context.Context, id uuid.UUID, lastError json.RawMessage, timestamp time.Time) (uuid.UUID, uuid.UUID, error) {
	query := `UPDATE tasks
		SET retry_count = COALESCE(retry_count, 0) + 1,
		status = CASE
			WHEN COALESCE(retry_count, 0) + 1 >= COALESCE(max_retry_count, 3) THEN 'failed'::task_status
			ELSE 'queued'::task_status
		END,
		assigned_node_id = CASE
			WHEN COALESCE(retry_count, 0) + 1 >= COALESCE(max_retry_count, 3) THEN assigned_node_id
			ELSE NULL
		END,
		last_error = $1,
		updated_at = $2
		WHERE id = $3 AND status NOT IN ('completed', 'failed') AND deleted_at IS NULL
		RETURNING id, status, workflow_run_id, workflow_step_id, workflow_id, chain_task
	`
	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadUncommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, query, lastError, timestamp, id)
	var completedTaskID uuid.UUID
	var status TaskStatus
	var workflowRunId, workflowStepId, workflowId *uuid.UUID
	var chainTask bool
	if err := row.Scan(&completedTaskID, &status, &workflowRunId, &workflowStepId, &workflowId, &chainTask); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.Commit(ctx); err != nil {
				return uuid.Nil, uuid.Nil, err
			}
			return id, uuid.Nil, nil
		}
		return uuid.Nil, uuid.Nil, err
	}

	if status != TaskStatusFailed {
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		return completedTaskID, uuid.Nil, nil
	}

	var workflowTaskID uuid.UUID

	if chainTask {
		nextTaskID, err := s.progressTaskChain(ctx, tx, completedTaskID, "on_failure", nil)
		if err != nil {
			return completedTaskID, uuid.Nil, err
		}
		if nextTaskID != uuid.Nil {
			workflowTaskID = nextTaskID
		}
	}

	if workflowStepId != nil && workflowId != nil {
		nextTaskID, err := s.progressWorkflowStep(ctx, tx, completedTaskID, workflowRunId, workflowId, workflowStepId, "on_failure", lastError)
		if err != nil {
			return completedTaskID, uuid.Nil, err
		}
		if nextTaskID == uuid.Nil {
			if err := tx.Commit(ctx); err != nil {
				return uuid.Nil, uuid.Nil, err
			}
			return completedTaskID, uuid.Nil, nil
		}
		workflowTaskID = nextTaskID
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	return completedTaskID, workflowTaskID, nil
}

func (s *service) CreateTask(ctx context.Context,
	payloadSlug string, payload json.RawMessage,
	runID *uuid.UUID, stepID *uuid.UUID,
	workflowID *uuid.UUID, unit *TaskUnit, chainTask bool) (uuid.UUID, error) {

	query := `INSERT INTO tasks (
		workflow_run_id, workflow_step_id, workflow_id, payload_slug, payload, allocated_unit, status, chain_task
	)
	SELECT 
		$1,
		$2,
		$3,
		$4::varchar,
		$5,
		COALESCE($6, w.task_unit),
		'queued'::task_status,
		$7
	FROM workers w
	WHERE w.slug = $4::varchar
	RETURNING id
	`
	row := s.pool.QueryRow(ctx, query, runID, stepID, workflowID, payloadSlug, payload, unit, chainTask)
	var retID uuid.UUID
	if err := row.Scan(&retID); err != nil {
		return uuid.Nil, err
	}

	return retID, nil
}

func (s *service) CreateTasks(ctx context.Context, tx pgx.Tx, tasks []Task) error {
	identifier := pgx.Identifier{"tasks"}
	columns := []string{
		"id", "workflow_run_id", "workflow_step_id", "workflow_id",
		"payload_slug", "payload", "allocated_unit", "status", "chain_task",
	}

	rowSrc := pgx.CopyFromSlice(len(tasks), func(i int) ([]any, error) {
		taskID := tasks[i].ID
		if taskID == uuid.Nil {
			var err error
			taskID, err = uuid.NewV7()
			if err != nil {
				taskID = uuid.New()
			}
		}
		return []any{
			taskID,
			tasks[i].WorkflowRunID,
			tasks[i].WorkflowStepID,
			tasks[i].WorkflowID,
			tasks[i].PayloadSlug,
			tasks[i].Payload,
			tasks[i].AllocatedUnit,
			tasks[i].Status,
			tasks[i].ChainTask,
		}, nil
	})

	_, err := tx.CopyFrom(ctx, identifier, columns, rowSrc)
	return err
}

func (s *service) CreateTaskChains(ctx context.Context, tx pgx.Tx, chains []TaskChain) error {
	identifier := pgx.Identifier{"task_chains"}
	columns := []string{
		"trigger_task_id", "follow_on_task_id", "triggerer_payload", "condition",
	}

	rowSrc := pgx.CopyFromSlice(len(chains), func(i int) ([]any, error) {
		payload := chains[i].TriggererPayload
		if len(payload) == 0 {
			payload = json.RawMessage(`{}`)
		}
		return []any{
			chains[i].TriggerTaskID,
			chains[i].FollowOnTaskID,
			payload,
			chains[i].Condition,
		}, nil
	})

	_, err := tx.CopyFrom(ctx, identifier, columns, rowSrc)
	return err
}

func (s *service) CreateTaskChain(ctx context.Context, steps []Step) ([]uuid.UUID, error) {
	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	rowsWorkers, err := tx.Query(ctx, "SELECT slug, task_unit FROM workers")
	if err != nil {
		return nil, err
	}
	defer rowsWorkers.Close()

	workerUnits := make(map[string]TaskUnit)
	for rowsWorkers.Next() {
		var slug string
		var unit TaskUnit
		if err := rowsWorkers.Scan(&slug, &unit); err != nil {
			return nil, err
		}
		workerUnits[slug] = unit
	}

	tasks := make([]Task, len(steps))
	stepIndexToTaskID := make(map[int]uuid.UUID)
	taskIDs := make([]uuid.UUID, len(steps))

	for i, step := range steps {
		taskID, err := uuid.NewV7()
		if err != nil {
			return nil, err
		}
		stepIndexToTaskID[i] = taskID
		taskIDs[i] = taskID

		status := TaskStatusPending
		if step.StepOrder == 1 {
			status = TaskStatusQueued
		}

		unit, ok := workerUnits[step.Slug]
		if !ok {
			unit = TaskUnitCPU
		}

		tasks[i] = Task{
			ID:            taskID,
			PayloadSlug:   step.Slug,
			Payload:       step.Payload,
			Status:        status,
			AllocatedUnit: unit,
			ChainTask:     true,
		}
	}

	if err := s.CreateTasks(ctx, tx, tasks); err != nil {
		return nil, err
	}

	var chains []TaskChain
	for i := 0; i < len(steps); i++ {
		currStep := steps[i]
		currTaskID := stepIndexToTaskID[i]

		for j, nextStep := range steps {
			if nextStep.StepOrder == currStep.StepOrder+1 {
				nextTaskID := stepIndexToTaskID[j]
				chains = append(chains, TaskChain{
					TriggerTaskID:  currTaskID,
					FollowOnTaskID: nextTaskID,
					Condition:      nextStep.TriggerCondition,
				})
			}
		}
	}

	if len(chains) > 0 {
		if err := s.CreateTaskChains(ctx, tx, chains); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return taskIDs, nil
}

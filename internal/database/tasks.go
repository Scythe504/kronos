package database

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *service) GetTask(ctx context.Context, taskId string) (Task, error) {
	query := `SELECT
			id, workflow_run_id, workflow_step_id, payload_slug, payload, retry_count, max_retry_count, 
			last_error, next_retry_at, status, allocated_unit, assigned_node_id,
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

func (s *service) GetTasks(ctx context.Context, machineID string) ([]Task, error) {
	query := `UPDATE tasks
			SET status = $1, assigned_node_id = $2, updated_at = now()
			WHERE id IN (
				SELECT id
				FROM tasks
				WHERE status = 'queued' AND (next_retry_at IS NULL OR next_retry_at <= now()) AND deleted_at IS NULL
				ORDER BY created_at ASC
				LIMIT 20
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, workflow_run_id, workflow_step_id, payload_slug, payload, retry_count, max_retry_count, 
				last_error, next_retry_at, status, allocated_unit, assigned_node_id,
				created_at, updated_at, deleted_at
		`
	var tasks []Task
	rows, err := s.pool.Query(ctx, query, TaskStatusRunning, machineID)
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

func (s *service) CompleteTask(ctx context.Context, id uuid.UUID, timestamp time.Time, outputPayload json.RawMessage) (uuid.UUID, uuid.UUID, error) {
	query := `UPDATE tasks t
		SET status = $1, updated_at = $2
		FROM task_chains tc
		WHERE t.id = tc.trigger_task_id
			AND t.id = $3 AND t.deleted_at IS NULL
		RETURNING t.id, t.workflow_step_id, t.workflow_id, 
		t.workflow_run_id, tc.follow_on_task_id
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
	var completedTaskID, workflowId, workflowStepId, workflowRunId, followOnTaskID uuid.UUID
	if err := row.Scan(&completedTaskID, &workflowStepId, &workflowId, &workflowRunId, &followOnTaskID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}

	if (workflowStepId == uuid.Nil && workflowId == uuid.Nil) && followOnTaskID == uuid.Nil {
		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, uuid.Nil, err
		}
		return completedTaskID, uuid.Nil, nil
	}

	trimmedPayload := bytes.TrimSpace(outputPayload)

	// An output exists ONLY if it has bytes AND isn't the literal word "null"
	hasOutput := len(trimmedPayload) > 0 && !bytes.Equal(trimmedPayload, []byte("null"))

	var payloadParam any = nil
	if hasOutput {
		payloadParam = outputPayload
	}

	if followOnTaskID != uuid.Nil {
		queryQueueNextTask := `UPDATE tasks
			SET status = $1, updated_at = now(), payload = COALESCE($3, payload)
			WHERE id = $2 AND deleted_at IS NULL
			RETURNING id
		`
		var chainId uuid.UUID

		args := []any{TaskStatusQueued, followOnTaskID, payloadParam}
		row := tx.QueryRow(ctx, queryQueueNextTask, args...)
		if err := row.Scan(&chainId); err != nil {
			return completedTaskID, uuid.Nil, err
		}

		if err := tx.Commit(ctx); err != nil {
			return uuid.Nil, uuid.Nil, err
		}

		return completedTaskID, chainId, nil
	}

	if workflowRunId == uuid.Nil {
		workflowRunId, err = s.createWorkflowRun(ctx, tx, workflowId)
		if err != nil {
			return completedTaskID, uuid.Nil, err
		}
	}

	queryCreateNextTask := `INSERT INTO tasks (
		workflow_run_id, workflow_id, workflow_step_id, 
		payload_slug,status, allocated_unit, payload
	)
	SELECT $1, $2, next_ws.id, next_ws.slug, $4, w.task_unit, $5
	FROM workers w
	CROSS JOIN workflow_steps current_ws
	INNER JOIN workflow_steps next_ws
		ON current_ws.workflow_id = next_ws.workflow_id
		AND next_ws.step_order = current_ws.step_order + 1
		AND next_ws.condition = 'on_success'
	WHERE w.slug = next_ws.slug
		AND current_ws.id = $3
		AND current_ws.workflow_id = $2
	RETURNING id
	`

	args := []any{workflowRunId, workflowId, workflowStepId, TaskStatusQueued, payloadParam}

	var workflowTaskID uuid.UUID
	row = tx.QueryRow(ctx, queryCreateNextTask, args...)
	if err := row.Scan(&workflowTaskID); err != nil {
		return completedTaskID, uuid.Nil, err
	}

	return completedTaskID, workflowTaskID, nil
}

func (s *service) FailTask(ctx context.Context, id uuid.UUID, lastError json.RawMessage, timestamp time.Time) (uuid.UUID, error) {
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
		WHERE id = $3 AND deleted_at IS NULL
		RETURNING id
	`
	row := s.pool.QueryRow(ctx, query, lastError, timestamp, id)
	var completedTaskID uuid.UUID
	if err := row.Scan(&completedTaskID); err != nil {
		return uuid.Nil, err
	}

	return completedTaskID, nil
}

func (s *service) CreateTask(ctx context.Context, payloadSlug string, payload json.RawMessage, runID *uuid.UUID, stepID *uuid.UUID, unit *TaskUnit) (uuid.UUID, error) {
	query := `INSERT INTO tasks (
		workflow_run_id, workflow_step_id, payload_slug, payload, allocated_unit, status
	)
	SELECT 
		$1,
		$2,
		$3,
		$4,
		COALESCE($5, w.task_unit),
		'queued'::task_status
	FROM workers w
	WHERE w.slug = $3
	RETURNING id
	`
	row := s.pool.QueryRow(ctx, query, runID, stepID, payloadSlug, payload, unit)
	var retID uuid.UUID
	if err := row.Scan(&retID); err != nil {
		return uuid.Nil, err
	}

	return retID, nil
}

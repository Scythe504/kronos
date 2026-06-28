package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *service) GetTask(ctx context.Context, taskId string) (Task, error) {
	query := `SELECT
			id, payload_slug, payload, retry_count, max_retry_count, 
			last_error, execution_schedule_time, execution_interval_seconds,
			cron_expression, next_retry_at, task_type, status, allocated_unit, assigned_node_id,
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
			RETURNING id, payload_slug, payload, retry_count, max_retry_count, 
				last_error, execution_schedule_time, execution_interval_seconds,
				cron_expression, next_retry_at, task_type, status, allocated_unit, assigned_node_id,
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

func (s *service) CompleteTask(ctx context.Context, id uuid.UUID, timestamp time.Time) (uuid.UUID, error) {
	query := `UPDATE tasks
		SET status = $1, updated_at = $2
		WHERE id = $3 AND deleted_at IS NULL
		RETURNING id
	`

	row := s.pool.QueryRow(ctx, query, TaskStatusCompleted, timestamp, id)
	var retId uuid.UUID
	if err := row.Scan(&retId); err != nil {
		return uuid.Nil, err
	}

	return retId, nil
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
	var retId uuid.UUID
	if err := row.Scan(&retId); err != nil {
		return uuid.Nil, err
	}

	return retId, nil
}

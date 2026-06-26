package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (t *Task) Scan(row pgx.Row) error {
	return row.Scan(
		&t.ID, &t.PayloadSlug, &t.Payload, &t.RetryCount,
		&t.MaxRetryCount, &t.LastError, &t.ExecutionScheduleTime,
		&t.ExecutionIntervalSeconds, &t.CronExpression, &t.TaskType,
		&t.Status, &t.AllocatedUnit, &t.CreatedAt, &t.UpdatedAt, &t.DeletedAt,
	)
}

func (s *service) GetTask(ctx context.Context) ([]Task, error) {
	query := `UPDATE tasks
			SET status = $1
			WHERE id IN (
				SELECT id
				FROM tasks
				WHERE status = 'queued' AND deleted_at IS NULL
				ORDER BY created_at ASC
				LIMIT 50
				FOR UPDATE SKIP LOCKED
			)
			RETURNING id, payload_slug, payload, retry_count, max_retry_count, 
				last_error, execution_schedule_time, execution_interval_seconds,
				cron_expression, task_type, status, allocated_unit, 
				created_at, updated_at, deleted_at
		`
	var tasks []Task
	rows, err := s.pool.Query(ctx, query, TaskStatusRunning)
	if err != nil {
		return tasks, err
	}

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

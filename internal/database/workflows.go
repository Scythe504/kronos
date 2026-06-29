package database

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"
)

type WorkflowPayload struct {
	Name          string `json:"slug"`
	TriggerType   string `json:"trigger_type,omitempty"`
	TriggerConfig string `json:"trigger_config,omitempty"`
	Steps         []Step `json:"chains"`
}

type Step struct {
	Slug             string           `json:"slug"`
	StepOrder        int              `json:"step_order"`
	TriggerCondition TriggerCondition `json:"trigger_condition,omitempty"`
	Payload          json.RawMessage  `json:"payload"`
}

func (s *service) CreateWorkflowTemplate(ctx context.Context, wp WorkflowPayload) (uuid.UUID, error) {
	queryInsertWorkflow := `INSERT INTO workflows (
		name,
		trigger_type,
		trigger_config
	) VALUES ($1, $2, $3)
		RETURNING id
	`

	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadUncommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	var workflowRetId uuid.UUID
	row := tx.QueryRow(ctx, queryInsertWorkflow, wp.Name, wp.TriggerType, wp.TriggerConfig)
	if err := row.Scan(&workflowRetId); err != nil {
		return uuid.Nil, err
	}

	identifier := pgx.Identifier{"workflow_steps"}
	columns := []string{"workflow_id", "slug", "step_order", "condition", "payload"}

	rowSrc := pgx.CopyFromSlice(len(wp.Steps), func(i int) ([]any, error) {
		return []any{
			workflowRetId,
			wp.Steps[i].Slug,
			wp.Steps[i].StepOrder,
			wp.Steps[i].TriggerCondition,
			wp.Steps[i].Payload,
		}, nil
	})
	n, err := tx.CopyFrom(ctx, identifier, columns, rowSrc)
	if err != nil {
		return uuid.Nil, err
	}
	if n != int64(len(wp.Steps)) {
		return uuid.Nil, errors.New("workflow steps count not match rows inserted")
	}
	tx.Commit(ctx)
	return workflowRetId, nil
}

func (s *service) CompleteWorkflowRun(ctx context.Context, workflowRunID uuid.UUID, workflowID uuid.UUID) (uuid.UUID, error) {
	query := `UPDATE workflow_runs
		SET status = 'success', updated_at = now()
		WHERE id = $1 AND workflow_id = $2
		RETURNING id
	`
	row := s.pool.QueryRow(ctx, query, workflowRunID, workflowID)
	var retID uuid.UUID
	if err := row.Scan(&retID); err != nil {
		return uuid.Nil, err
	}

	return retID, nil
}

func (s *service) TriggerWorkflow(ctx context.Context, workflowID uuid.UUID) (uuid.UUID, error) {
	opts := pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadWrite,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	runID, err := s.createWorkflowRun(ctx, tx, workflowID)
	if err != nil {
		return uuid.Nil, err
	}

	var triggerType TriggerType
	var triggerConfig json.RawMessage
	queryW := `SELECT trigger_type, trigger_config FROM workflows WHERE id = $1 AND deleted_at IS NULL`
	err = tx.QueryRow(ctx, queryW, workflowID).Scan(&triggerType, &triggerConfig)
	if err != nil {
		return uuid.Nil, err
	}

	if triggerType == TriggerTypeCron {
		var cfg struct {
			CronExpression string `json:"cron_expression"`
			Cron           string `json:"cron"`
			Expression     string `json:"expression"`
		}
		_ = json.Unmarshal(triggerConfig, &cfg)
		expr := cfg.CronExpression
		if expr == "" {
			expr = cfg.Cron
		}
		if expr == "" {
			expr = cfg.Expression
		}

		if expr != "" {
			sched, err := cron.ParseStandard(expr)
			if err != nil {
				return uuid.Nil, err
			}
			nextRun := sched.Next(time.Now())
			_, err = tx.Exec(ctx, `UPDATE workflows SET next_run_at = $1, updated_at = now() WHERE id = $2`, nextRun, workflowID)
			if err != nil {
				return uuid.Nil, err
			}
		}
	}

	query := `SELECT ws.id, ws.slug, ws.default_payload 
		FROM workflow_steps ws
		INNER JOIN workflows w ON ws.workflow_id = w.id
		WHERE ws.workflow_id = $1 AND ws.step_order = 1 AND ws.condition = 'on_success'
		  AND w.deleted_at IS NULL`
	
	rows, err := tx.Query(ctx, query, workflowID)
	if err != nil {
		return uuid.Nil, err
	}

	st, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[WorkflowStep])
	if err != nil {
		return uuid.Nil, err
	}

	queryTask := `INSERT INTO tasks (workflow_run_id, workflow_step_id, payload_slug, payload, allocated_unit, status)
		SELECT $1, $2, $3, $4, w.task_unit, 'queued'::task_status
		FROM workers w WHERE w.slug = $3`
	_, err = tx.Exec(ctx, queryTask, runID, st.ID, st.Slug, st.DefaultPayload)
	if err != nil {
		return uuid.Nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}

	return runID, nil
}

func (s *service) createWorkflowRun(ctx context.Context, tx pgx.Tx, workflowID uuid.UUID) (uuid.UUID, error) {
	query := `INSERT INTO workflow_runs (workflow_id)
		SELECT id FROM workflows
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING id
	`
	row := tx.QueryRow(ctx, query, workflowID)
	var runID uuid.UUID
	if err := row.Scan(&runID); err != nil {
		return uuid.Nil, err
	}

	return runID, nil
}

func (s *service) TriggerDueCronWorkflows(ctx context.Context) ([]uuid.UUID, error) {
	opts := pgx.TxOptions{
		IsoLevel:   pgx.ReadCommitted,
		AccessMode: pgx.ReadWrite,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	query := `SELECT id, name, trigger_config
		FROM workflows
		WHERE trigger_type = 'cron'
		  AND (next_run_at IS NULL OR next_run_at <= now())
		  AND deleted_at IS NULL
		LIMIT 30
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}

	type dueWorkflow struct {
		ID            uuid.UUID
		Name          string
		TriggerConfig json.RawMessage
	}

	var dueWorkflows []dueWorkflow
	for rows.Next() {
		var w dueWorkflow
		if err := rows.Scan(&w.ID, &w.Name, &w.TriggerConfig); err != nil {
			rows.Close()
			return nil, err
		}
		dueWorkflows = append(dueWorkflows, w)
	}
	rows.Close()

	var runIDs []uuid.UUID

	for _, w := range dueWorkflows {
		var cfg struct {
			CronExpression string `json:"cron_expression"`
			Cron           string `json:"cron"`
			Expression     string `json:"expression"`
		}
		_ = json.Unmarshal(w.TriggerConfig, &cfg)
		expr := cfg.CronExpression
		if expr == "" {
			expr = cfg.Cron
		}
		if expr == "" {
			expr = cfg.Expression
		}

		nextRun := time.Now().Add(1 * time.Hour)
		if expr != "" {
			sched, err := cron.ParseStandard(expr)
			if err != nil {
				nextRun = time.Now().Add(5 * time.Minute)
			} else {
				nextRun = sched.Next(time.Now())
			}
		}

		_, err = tx.Exec(ctx, `UPDATE workflows SET next_run_at = $1, updated_at = now() WHERE id = $2`, nextRun, w.ID)
		if err != nil {
			return nil, err
		}

		runID, err := s.createWorkflowRun(ctx, tx, w.ID)
		if err != nil {
			return nil, err
		}

		stepQuery := `SELECT ws.id, ws.slug, ws.default_payload 
			FROM workflow_steps ws
			INNER JOIN workflows w ON ws.workflow_id = w.id
			WHERE ws.workflow_id = $1 AND ws.step_order = 1 AND ws.condition = 'on_success'
			  AND w.deleted_at IS NULL`
		
		var stepID uuid.UUID
		var slug string
		var defaultPayload []byte
		err = tx.QueryRow(ctx, stepQuery, w.ID).Scan(&stepID, &slug, &defaultPayload)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				runIDs = append(runIDs, runID)
				continue
			}
			return nil, err
		}

		queryTask := `INSERT INTO tasks (workflow_run_id, workflow_step_id, payload_slug, payload, allocated_unit, status)
			SELECT $1, $2, $3, $4, w.task_unit, 'queued'::task_status
			FROM workers w WHERE w.slug = $3`
		_, err = tx.Exec(ctx, queryTask, runID, stepID, slug, defaultPayload)
		if err != nil {
			return nil, err
		}

		runIDs = append(runIDs, runID)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return runIDs, nil
}

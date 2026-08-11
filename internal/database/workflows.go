package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/robfig/cron/v3"
)

type WorkflowPayload struct {
	Name          string   `json:"slug"`
	TriggerType   string   `json:"trigger_type,omitempty"`
	TriggerConfig string   `json:"trigger_config,omitempty"`
	Steps         []Step   `json:"chains"`
	Workers       []Worker `json:"workers,omitempty"`
}

type Step struct {
	Slug             string           `json:"slug"`
	StepOrder        int              `json:"step_order"`
	TriggerCondition TriggerCondition `json:"trigger_condition,omitempty"`
	Payload          json.RawMessage  `json:"payload"`
	Worker           *Worker          `json:"worker,omitempty"`
}

func (s *service) CreateWorkflowTemplate(ctx context.Context, wp WorkflowPayload) (uuid.UUID, error) {
	var nextRun *time.Time
	triggerConfigStr := wp.TriggerConfig
	if wp.TriggerType == string(TriggerTypeCron) && wp.TriggerConfig != "" {
		expr := wp.TriggerConfig
		var cfg struct {
			CronExpression string `json:"cron_expression"`
			Cron           string `json:"cron"`
			Expression     string `json:"expression"`
		}
		if err := json.Unmarshal([]byte(wp.TriggerConfig), &cfg); err == nil {
			if cfg.CronExpression != "" {
				expr = cfg.CronExpression
			} else if cfg.Cron != "" {
				expr = cfg.Cron
			} else if cfg.Expression != "" {
				expr = cfg.Expression
			}
		} else {
			// Raw cron expression string sent from frontend (e.g. "*/30 * * * *")
			triggerConfigStr = fmt.Sprintf(`{"cron_expression":%q}`, expr)
		}

		if expr != "" {
			if sched, err := cron.ParseStandard(expr); err == nil {
				t := sched.Next(time.Now())
				nextRun = &t
			}
		}
	}

	queryInsertWorkflow := `INSERT INTO workflows (
		name,
		trigger_type,
		trigger_config,
		next_run_at
	) VALUES ($1, $2, $3, $4)
		RETURNING id
	`

	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return uuid.Nil, err
	}
	defer tx.Rollback(ctx)

	// Collect and upsert any worker definitions included in the workflow template
	var workersToUpsert []Worker
	workersToUpsert = append(workersToUpsert, wp.Workers...)
	for _, step := range wp.Steps {
		if step.Worker != nil {
			workersToUpsert = append(workersToUpsert, *step.Worker)
		}
	}

	if len(workersToUpsert) > 0 {
		_, err := s.UpsertWorker(ctx, tx, workersToUpsert)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to upsert workers for workflow template: %w", err)
		}
	}

	var workflowRetId uuid.UUID
	row := tx.QueryRow(ctx, queryInsertWorkflow, wp.Name, wp.TriggerType, triggerConfigStr, nextRun)
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
	if err := tx.Commit(ctx); err != nil {
		return uuid.Nil, err
	}
	return workflowRetId, nil
}

func (s *service) CompleteWorkflowRun(ctx context.Context, workflowRunID uuid.UUID, workflowID uuid.UUID) (uuid.UUID, error) {
	query := `UPDATE workflow_runs
		SET status = 'success', updated_at = now()
		WHERE id = $1 AND workflow_id = $2
		RETURNING id
	`
	row := s.pool.QueryRow(ctx, query, workflowRunID, workflowID)
	var runRetId uuid.UUID
	if err := row.Scan(&runRetId); err != nil {
		return uuid.Nil, err
	}
	return runRetId, nil
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

	query := `SELECT ws.id, ws.slug, ws.payload 
		FROM workflow_steps ws
		INNER JOIN workflows w ON ws.workflow_id = w.id
		WHERE ws.workflow_id = $1 AND ws.step_order = 1 AND ws.condition = 'on_success'
		  AND w.deleted_at IS NULL`
	
	var stepID uuid.UUID
	var slug string
	var payload json.RawMessage

	err = tx.QueryRow(ctx, query, workflowID).Scan(&stepID, &slug, &payload)
	if err != nil {
		return uuid.Nil, err
	}

	queryTask := `INSERT INTO tasks (workflow_run_id, workflow_step_id, workflow_id, payload_slug, payload, allocated_unit, status)
		SELECT $1, $2, $3, $4::varchar, $5, w.task_unit, 'queued'::task_status
		FROM workers w WHERE w.slug = $4::varchar`
	_, err = tx.Exec(ctx, queryTask, runID, stepID, workflowID, slug, payload)
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

	query := `SELECT id, trigger_config
		FROM workflows
		WHERE trigger_type = 'cron' AND (next_run_at IS NULL OR next_run_at <= now()) AND deleted_at IS NULL
		LIMIT 30
		FOR UPDATE SKIP LOCKED`

	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type dueWorkflow struct {
		ID            uuid.UUID       `db:"id"`
		TriggerConfig json.RawMessage `db:"trigger_config"`
	}

	dueWorkflows, err := pgx.CollectRows(rows, pgx.RowToStructByName[dueWorkflow])
	if err != nil {
		return nil, err
	}

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

		stepQuery := `SELECT ws.id, ws.slug, ws.payload 
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

		queryTask := `INSERT INTO tasks (workflow_run_id, workflow_step_id, workflow_id, payload_slug, payload, allocated_unit, status)
			SELECT $1, $2, $3, $4::varchar, $5, w.task_unit, 'queued'::task_status
			FROM workers w WHERE w.slug = $4::varchar`
		_, err = tx.Exec(ctx, queryTask, runID, stepID, w.ID, slug, defaultPayload)
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

func (s *service) GetWorkflowTemplate(ctx context.Context, id string) (Workflow, error) {
	workflowUUID, err := uuid.Parse(id)
	if err != nil {
		return Workflow{}, err
	}
	query := `SELECT id, name, trigger_type, trigger_config, next_run_at, created_at, updated_at, deleted_at FROM workflows WHERE id = $1 AND deleted_at IS NULL`
	rows, err := s.pool.Query(ctx, query, workflowUUID)
	if err != nil {
		return Workflow{}, err
	}
	defer rows.Close()
	wf, err := pgx.CollectOneRow(rows, pgx.RowToStructByName[Workflow])
	if err != nil {
		return Workflow{}, err
	}

	stepsQuery := `SELECT id, workflow_id, slug, condition, step_order, payload FROM workflow_steps WHERE workflow_id = $1 ORDER BY step_order ASC`
	stepRows, err := s.pool.Query(ctx, stepsQuery, workflowUUID)
	if err != nil {
		return Workflow{}, err
	}
	defer stepRows.Close()

	steps, err := pgx.CollectRows(stepRows, pgx.RowToStructByName[WorkflowStep])
	if err != nil {
		return Workflow{}, err
	}
	wf.Steps = steps
	return wf, nil
}

func (s *service) GetWorkflowTemplates(ctx context.Context, page, perPage int) ([]Workflow, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	offset := (page - 1) * perPage

	query := `SELECT id, name, trigger_type, trigger_config, next_run_at, created_at, updated_at, deleted_at FROM workflows WHERE deleted_at IS NULL ORDER BY created_at DESC LIMIT $1 OFFSET $2`
	rows, err := s.pool.Query(ctx, query, perPage, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, pgx.RowToStructByName[Workflow])
}

func (s *service) DeleteWorkflowTemplate(ctx context.Context, id string) (string, error) {
	workflowUUID, err := uuid.Parse(id)
	if err != nil {
		return "", err
	}
	query := `UPDATE workflows SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL RETURNING id`
	var deletedID uuid.UUID
	err = s.pool.QueryRow(ctx, query, workflowUUID).Scan(&deletedID)
	if err != nil {
		return "", err
	}
	return deletedID.String(), nil
}

func (s *service) UpdateWorkflowTemplate(ctx context.Context, id uuid.UUID, wp WorkflowPayload) error {
	var nextRun *time.Time
	triggerConfigStr := wp.TriggerConfig
	if wp.TriggerType == string(TriggerTypeCron) && wp.TriggerConfig != "" {
		expr := wp.TriggerConfig
		var cfg struct {
			CronExpression string `json:"cron_expression"`
			Cron           string `json:"cron"`
			Expression     string `json:"expression"`
		}
		if err := json.Unmarshal([]byte(wp.TriggerConfig), &cfg); err == nil {
			if cfg.CronExpression != "" {
				expr = cfg.CronExpression
			} else if cfg.Cron != "" {
				expr = cfg.Cron
			} else if cfg.Expression != "" {
				expr = cfg.Expression
			}
		} else {
			triggerConfigStr = fmt.Sprintf(`{"cron_expression":%q}`, expr)
		}

		if expr != "" {
			if sched, err := cron.ParseStandard(expr); err == nil {
				t := sched.Next(time.Now())
				nextRun = &t
			}
		}
	}

	queryUpdateWorkflow := `UPDATE workflows SET 
		name = $1, 
		trigger_type = $2, 
		trigger_config = $3, 
		next_run_at = $4,
		updated_at = now()
	WHERE id = $5 AND deleted_at IS NULL`

	opts := pgx.TxOptions{
		IsoLevel:       pgx.ReadCommitted,
		AccessMode:     pgx.ReadWrite,
		DeferrableMode: pgx.NotDeferrable,
	}
	tx, err := s.pool.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Collect and upsert any worker definitions included in the workflow template
	var workersToUpsert []Worker
	workersToUpsert = append(workersToUpsert, wp.Workers...)
	for _, step := range wp.Steps {
		if step.Worker != nil {
			workersToUpsert = append(workersToUpsert, *step.Worker)
		}
	}

	if len(workersToUpsert) > 0 {
		_, err := s.UpsertWorker(ctx, tx, workersToUpsert)
		if err != nil {
			return fmt.Errorf("failed to upsert workers for workflow template: %w", err)
		}
	}

	_, err = tx.Exec(ctx, queryUpdateWorkflow, wp.Name, wp.TriggerType, triggerConfigStr, nextRun, id)
	if err != nil {
		return err
	}

	// Delete existing steps
	_, err = tx.Exec(ctx, `DELETE FROM workflow_steps WHERE workflow_id = $1`, id)
	if err != nil {
		return err
	}

	// Copy in new steps
	identifier := pgx.Identifier{"workflow_steps"}
	columns := []string{"workflow_id", "slug", "step_order", "condition", "payload"}

	rowSrc := pgx.CopyFromSlice(len(wp.Steps), func(i int) ([]any, error) {
		return []any{
			id,
			wp.Steps[i].Slug,
			wp.Steps[i].StepOrder,
			wp.Steps[i].TriggerCondition,
			wp.Steps[i].Payload,
		}, nil
	})
	_, err = tx.CopyFrom(ctx, identifier, columns, rowSrc)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

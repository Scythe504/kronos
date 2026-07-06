package database

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	rcron "github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"path/filepath"
)

// setupTestDB handles environment setup and database initialization with Testcontainers fallback.
func setupTestDB(t *testing.T) (Service, *service, context.Context) {
	ctx := context.Background()

	envPath := ".env"
	for range 5 {
		if _, err := os.Stat(envPath); err == nil {
			_ = godotenv.Load(envPath)
			break
		}
		envPath = filepath.Join("..", envPath)
	}

	dbURL := os.Getenv("DB_URL")
	var pgContainer *postgres.PostgresContainer

	useTestcontainers := false
	if dbURL == "" {
		useTestcontainers = true
	} else {
		config, err := pgxpool.ParseConfig(dbURL)
		if err == nil {
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err == nil {
				err = pool.Ping(ctx)
				pool.Close()
			}
			if err != nil {
				useTestcontainers = true
			}
		} else {
			useTestcontainers = true
		}
	}

	if useTestcontainers {
		var err error
		pgContainer, err = postgres.Run(ctx,
			"postgres:17-alpine",
			postgres.WithDatabase("kronos_test"),
			postgres.WithUsername("postgres"),
			postgres.WithPassword("password"),
			testcontainers.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").
					WithOccurrence(2).
					WithStartupTimeout(15*time.Second),
			),
		)
		if err != nil {
			t.Fatalf("failed to start postgres container: %v", err)
		}
		t.Cleanup(func() {
			if err := testcontainers.TerminateContainer(pgContainer); err != nil {
				t.Fatalf("failed to terminate container: %v", err)
			}
		})

		dbURL, err = pgContainer.ConnectionString(ctx, "sslmode=disable")
		if err != nil {
			t.Fatalf("failed to get connection string: %v", err)
		}
		t.Setenv("DB_URL", dbURL)
		os.Setenv("DB_URL", dbURL)
	}

	dbService := New(ctx)
	s := dbService.(*service)

	// Clean up tables to ensure test independence
	_, _ = s.pool.Exec(ctx, "TRUNCATE tasks, workflow_runs, workflow_steps, workflows, workers, nodes CASCADE")

	return dbService, s, ctx
}

func TestCreateWorkflowTemplate(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
	`, "video_transcode", "Video Transcoder", "./transcoder", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	wp := WorkflowPayload{
		Name:          "video_processing_flow",
		TriggerType:   "webhook",
		TriggerConfig: `{"auth": "token"}`,
		Steps: []Step{
			{
				Slug:             "video_transcode",
				StepOrder:        1,
				TriggerCondition: TriggerConditionOnSuccess,
				Payload:          json.RawMessage(`{"resolution": "1080p"}`),
			},
		},
	}

	wfID, err := dbService.CreateWorkflowTemplate(ctx, wp)
	assert.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, wfID)

	var name string
	var triggerType string
	err = s.pool.QueryRow(ctx, "SELECT name, trigger_type FROM workflows WHERE id = $1", wfID).Scan(&name, &triggerType)
	assert.NoError(t, err)
	assert.Equal(t, "video_processing_flow", name)
	assert.Equal(t, "webhook", triggerType)

	var stepID uuid.UUID
	var slug string
	var stepOrder int
	var condition string
	err = s.pool.QueryRow(ctx, `
		SELECT id, slug, step_order, condition 
		FROM workflow_steps 
		WHERE workflow_id = $1
	`, wfID).Scan(&stepID, &slug, &stepOrder, &condition)

	assert.NoError(t, err)
	assert.NotEqual(t, uuid.Nil, stepID)
	assert.Equal(t, "video_transcode", slug)
	assert.Equal(t, 1, stepOrder)
	assert.Equal(t, "on_success", condition)
}

func TestTriggerWorkflow(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	t.Run("Webhook Triggered Workflow", func(t *testing.T) {
		_, _ = s.pool.Exec(ctx, "TRUNCATE tasks, workflow_runs, workflow_steps, workflows CASCADE")

		wp := WorkflowPayload{
			Name:          "webhook_flow",
			TriggerType:   "webhook",
			TriggerConfig: `{"auth": "token"}`,
			Steps: []Step{
				{
					Slug:             "test_slug",
					StepOrder:        1,
					TriggerCondition: TriggerConditionOnSuccess,
					Payload:          json.RawMessage(`{"param": 1}`),
				},
			},
		}

		wfID, err := dbService.CreateWorkflowTemplate(ctx, wp)
		assert.NoError(t, err)

		runID, err := dbService.TriggerWorkflow(ctx, wfID)
		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, runID)

		var runStatus string
		err = s.pool.QueryRow(ctx, "SELECT status FROM workflow_runs WHERE id = $1", runID).Scan(&runStatus)
		assert.NoError(t, err)
		assert.Equal(t, "queued", runStatus)

		var taskID uuid.UUID
		var taskStatus string
		var taskWorkflowRunID, taskWorkflowStepID, taskWorkflowID uuid.UUID
		err = s.pool.QueryRow(ctx, `
			SELECT id, status, workflow_run_id, workflow_step_id, workflow_id 
			FROM tasks 
			WHERE workflow_run_id = $1
		`, runID).Scan(&taskID, &taskStatus, &taskWorkflowRunID, &taskWorkflowStepID, &taskWorkflowID)

		assert.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, taskID)
		assert.Equal(t, "queued", taskStatus)
		assert.Equal(t, runID, taskWorkflowRunID)
		assert.Equal(t, wfID, taskWorkflowID)
	})

	t.Run("Cron Triggered Workflow", func(t *testing.T) {
		_, _ = s.pool.Exec(ctx, "TRUNCATE tasks, workflow_runs, workflow_steps, workflows CASCADE")

		cronExpr := "0 0 * * *"
		wp := WorkflowPayload{
			Name:          "cron_flow",
			TriggerType:   "cron",
			TriggerConfig: `{"cron_expression": "0 0 * * *"}`,
			Steps: []Step{
				{
					Slug:             "test_slug",
					StepOrder:        1,
					TriggerCondition: TriggerConditionOnSuccess,
					Payload:          json.RawMessage(`{"param": 2}`),
				},
			},
		}

		wfID, err := dbService.CreateWorkflowTemplate(ctx, wp)
		assert.NoError(t, err)

		startTime := time.Now()
		runID, err := dbService.TriggerWorkflow(ctx, wfID)
		assert.NoError(t, err)

		var runStatus string
		err = s.pool.QueryRow(ctx, "SELECT status FROM workflow_runs WHERE id = $1", runID).Scan(&runStatus)
		assert.NoError(t, err)
		assert.Equal(t, "queued", runStatus)

		var taskCount int
		err = s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE workflow_run_id = $1", runID).Scan(&taskCount)
		assert.NoError(t, err)
		assert.Equal(t, 1, taskCount)

		var nextRunAt *time.Time
		err = s.pool.QueryRow(ctx, "SELECT next_run_at FROM workflows WHERE id = $1", wfID).Scan(&nextRunAt)
		assert.NoError(t, err)
		assert.NotNil(t, nextRunAt)

		sched, err := rcron.ParseStandard(cronExpr)
		assert.NoError(t, err)
		expectedNext := sched.Next(startTime)

		assert.WithinDuration(t, expectedNext, *nextRunAt, 5*time.Second)
	})
}

func TestTriggerDueCronWorkflows(t *testing.T) {
	dbService, s, ctx := setupTestDB(t)

	_, err := s.pool.Exec(ctx, `
		INSERT INTO workers (slug, name, entrypoint, task_unit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (slug) DO UPDATE 
		SET name = EXCLUDED.name, entrypoint = EXCLUDED.entrypoint, task_unit = EXCLUDED.task_unit
	`, "test_slug", "Test Worker", "./test_worker", "cpu")
	if err != nil {
		t.Fatalf("failed to seed mock worker: %v", err)
	}

	wpDue := WorkflowPayload{
		Name:          "due_cron_flow",
		TriggerType:   "cron",
		TriggerConfig: `{"cron_expression": "*/5 * * * *"}`,
		Steps: []Step{
			{
				Slug:             "test_slug",
				StepOrder:        1,
				TriggerCondition: TriggerConditionOnSuccess,
				Payload:          json.RawMessage(`{"param": 1}`),
			},
		},
	}
	dueWfID, err := dbService.CreateWorkflowTemplate(ctx, wpDue)
	assert.NoError(t, err)

	pastTime := time.Now().Add(-10 * time.Minute)
	_, err = s.pool.Exec(ctx, "UPDATE workflows SET next_run_at = $1 WHERE id = $2", pastTime, dueWfID)
	assert.NoError(t, err)

	wpFuture := WorkflowPayload{
		Name:          "future_cron_flow",
		TriggerType:   "cron",
		TriggerConfig: `{"cron_expression": "0 0 1 1 *"}`,
		Steps: []Step{
			{
				Slug:             "test_slug",
				StepOrder:        1,
				TriggerCondition: TriggerConditionOnSuccess,
				Payload:          json.RawMessage(`{"param": 2}`),
			},
		},
	}
	futureWfID, err := dbService.CreateWorkflowTemplate(ctx, wpFuture)
	assert.NoError(t, err)

	futureTime := time.Now().Add(10 * time.Minute)
	_, err = s.pool.Exec(ctx, "UPDATE workflows SET next_run_at = $1 WHERE id = $2", futureTime, futureWfID)
	assert.NoError(t, err)

	runIDs, err := dbService.TriggerDueCronWorkflows(ctx)
	assert.NoError(t, err)
	assert.Len(t, runIDs, 1)

	var runStatus string
	err = s.pool.QueryRow(ctx, "SELECT status FROM workflow_runs WHERE id = $1", runIDs[0]).Scan(&runStatus)
	assert.NoError(t, err)
	assert.Equal(t, "queued", runStatus)

	var taskCount int
	err = s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM tasks WHERE workflow_run_id = $1", runIDs[0]).Scan(&taskCount)
	assert.NoError(t, err)
	assert.Equal(t, 1, taskCount)

	var nextRunAt *time.Time
	err = s.pool.QueryRow(ctx, "SELECT next_run_at FROM workflows WHERE id = $1", dueWfID).Scan(&nextRunAt)
	assert.NoError(t, err)
	assert.NotNil(t, nextRunAt)
	assert.True(t, nextRunAt.After(time.Now()))

	var futureNextRunAt *time.Time
	err = s.pool.QueryRow(ctx, "SELECT next_run_at FROM workflows WHERE id = $1", futureWfID).Scan(&futureNextRunAt)
	assert.NoError(t, err)
	assert.NotNil(t, futureNextRunAt)
	assert.WithinDuration(t, futureTime, *futureNextRunAt, 1*time.Second)
}

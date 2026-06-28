-- +goose Up
CREATE TYPE task_type AS ENUM ('cron', 'one_off', 'event_driven');
CREATE TYPE task_status AS ENUM ('failed', 'queued', 'running', 'completed', 'pending');
CREATE TABLE IF NOT EXISTS tasks (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  workflow_id UUID REFERENCES workflows(id),
  payload_slug VARCHAR(255) NOT NULL, -- REFERENCES workers(slug),
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  retry_count INT DEFAULT 0,
  max_retry_count INT DEFAULT 3,
  last_error JSONB,
  execution_schedule_time TIMESTAMPTZ,
  cron_expression VARCHAR(100) DEFAULT NULL,
  execution_interval_seconds INT DEFAULT 900,
  next_retry_at TIMESTAMPTZ,
  task_type task_type NOT NULL,
  status task_status NOT NULL DEFAULT 'queued',
  allocated_unit task_unit NOT NULL DEFAULT 'cpu',
  assigned_node_id VARCHAR(64) DEFAULT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ DEFAULT null
);
CREATE INDEX index_tasks_active ON tasks (status)
WHERE deleted_at IS NULL;
CREATE INDEX index_retry_count ON tasks (retry_count);
CREATE TABLE IF NOT EXISTS task_chains (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  trigger_task_id UUID NOT NULL REFERENCES tasks(id),
  follow_on_task_id UUID NOT NULL REFERENCES tasks(id),
  triggerer_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  condition trigger_condition NOT NULL DEFAULT 'on_success'
);
CREATE INDEX index_task_chains_trigger_task_id ON task_chains (trigger_task_id);
-- +goose Down
DROP INDEX IF EXISTS index_task_chains_trigger_task_id;
DROP TABLE IF EXISTS task_chains;
DROP INDEX IF EXISTS index_retry_count;
DROP INDEX IF EXISTS index_tasks_active;
DROP TABLE IF EXISTS tasks;
DROP TYPE IF EXISTS task_status;
DROP TYPE IF EXISTS task_type;
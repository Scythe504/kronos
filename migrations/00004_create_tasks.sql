-- +goose Up
CREATE TYPE task_status AS ENUM ('failed', 'queued', 'running', 'completed', 'pending');
CREATE TABLE IF NOT EXISTS tasks (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  workflow_run_id UUID REFERENCES workflow_runs(id) DEFAULT NULL,
  workflow_step_id UUID REFERENCES workflow_steps(id) DEFAULT NULL,
  workflow_id UUID REFERENCES workflows(id) DEFAULT NULL,
  payload_slug VARCHAR(255) NOT NULL, -- REFERENCES workers(slug),
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  retry_count INT DEFAULT 0,
  max_retry_count INT DEFAULT 3,
  last_error JSONB,
  next_retry_at TIMESTAMPTZ,
  status task_status NOT NULL DEFAULT 'queued',
  allocated_unit task_unit NOT NULL DEFAULT 'cpu',
  assigned_node_id VARCHAR(64) DEFAULT NULL,
  chain_task BOOLEAN NOT NULL DEFAULT FALSE,
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
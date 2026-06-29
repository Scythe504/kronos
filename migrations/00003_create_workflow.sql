-- +goose Up
CREATE TYPE trigger_type AS ENUM ('cron', 'webhook');
CREATE TYPE trigger_condition AS ENUM ('on_success', 'on_failure');
CREATE TYPE workflow_run_status AS ENUM ('success', 'failed', 'queued');

CREATE TABLE IF NOT EXISTS workflows (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  name VARCHAR(255) NOT NULL,
  trigger_type trigger_type NOT NULL DEFAULT 'webhook',
  trigger_config JSONB NOT NULL DEFAULT '{}'::jsonb,
  next_run_at TIMESTAMPTZ DEFAULT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ DEFAULT NULL
);
CREATE INDEX index_next_run_at ON workflows(next_run_at);

CREATE TABLE IF NOT EXISTS workflow_steps (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  workflow_id UUID NOT NULL REFERENCES workflows(id),
  slug VARCHAR(255) NOT NULL REFERENCES workers(slug),
  condition trigger_condition NOT NULL DEFAULT 'on_success',
  step_order INT NOT NULL,
  payload JSONB NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (workflow_id, step_order, condition)
);

CREATE TABLE IF NOT EXISTS workflow_runs (
  id UUID PRIMARY KEY DEFAULT uuidv7(),
  workflow_id UUID NOT NULL REFERENCES workflows(id),
  status workflow_run_status NOT NULL DEFAULT 'queued',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX index_workflow_steps_workflow_id ON workflow_steps (workflow_id);

-- +goose Down
DROP INDEX IF EXISTS index_workflow_steps_workflow_id;
DROP TABLE IF EXISTS workflow_runs;
DROP TABLE IF EXISTS workflow_steps;
DROP INDEX IF EXISTS index_next_run_at;
DROP TABLE IF EXISTS workflows;
DROP TYPE IF EXISTS workflow_run_status;
DROP TYPE IF EXISTS trigger_condition;
DROP TYPE IF EXISTS trigger_type;
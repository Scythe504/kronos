CREATE TYPE task_unit AS ENUM ('cpu', 'gpu');
CREATE TABLE IF NOT EXISTS workers (
  slug VARCHAR(255) PRIMARY KEY,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  repo_url VARCHAR(500) NOT NULL,
  repo_ref VARCHAR(100) NOT NULL,
  env_vars BYTEA,
  pre_build_command TEXT,
  build_command TEXT,
  run_command TEXT,
  dockerfile_path VARCHAR(500),
  entrypoint VARCHAR(500) NOT NULL,  -- path to executable
  task_unit task_unit NOT NULL DEFAULT 'cpu',
  task_timeout_seconds INT NOT NULL DEFAULT 300,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  deleted_at TIMESTAMPTZ DEFAULT NULL
);

-- +goose Down
DROP TABLE IF EXISTS workers;
DROP TYPE IF EXISTS task_unit;
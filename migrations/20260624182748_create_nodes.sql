-- +goose Up
CREATE TYPE node_status AS ENUM ('active', 'idle', 'dead', 'inactive');

CREATE TABLE IF NOT EXISTS nodes (
  machine_id VARCHAR(64) PRIMARY KEY,

  kernel VARCHAR(64) NOT NULL,
  architecture VARCHAR(20) NOT NULL,

  gpu_vram_kb BIGINT,
  gpu_model VARCHAR(64),
  cpu_model VARCHAR(64) NOT NULL,
  cpu_cores INT NOT NULL,
  ram_kb BIGINT NOT NULL,

  ip_addr VARCHAR(45) NOT NULL,
  hostname VARCHAR(255) NOT NULL,
  cloud_region VARCHAR(255),
  cloud_platform VARCHAR(45),

  task_unit task_unit NOT NULL,
  status node_status NOT NULL DEFAULT 'idle',

  node_version VARCHAR(20) NOT NULL,
  
  last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  registered_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS index_status ON nodes (status);
CREATE INDEX IF NOT EXISTS index_task_unit ON nodes (task_unit);

-- +goose Down
DROP INDEX IF EXISTS index_task_unit;
DROP INDEX IF EXISTS index_status;
DROP TABLE IF EXISTS nodes;
DROP TYPE IF EXISTS node_status;
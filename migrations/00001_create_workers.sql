-- +goose Up
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION uuidv7() RETURNS uuid AS $$
DECLARE
  v_time double precision := extract(epoch from clock_timestamp()) * 1000;
  v_unix_time bigint := v_time;
  v_time_hex text := lpad(to_hex(v_unix_time), 12, '0');
  v_rand_hex text := encode(gen_random_bytes(10), 'hex');
  v_raw text := v_time_hex || v_rand_hex;
BEGIN
  -- set version to 7 (bits 48-51 -> 0111 -> 7)
  v_raw := overlay(v_raw placing '7' from 13 for 1);
  -- set variant to 2 (bits 64-65 -> 10 -> '8', '9', 'a', or 'b')
  v_raw := overlay(v_raw placing '8' from 17 for 1);
  RETURN (
    substring(v_raw from 1 for 8) || '-' ||
    substring(v_raw from 9 for 4) || '-' ||
    substring(v_raw from 13 for 4) || '-' ||
    substring(v_raw from 17 for 4) || '-' ||
    substring(v_raw from 21 for 12)
  )::uuid;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

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
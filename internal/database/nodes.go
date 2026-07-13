package database

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

func (s *service) RegisterNode(ctx context.Context, n Node) (string, error) {
	query := `INSERT INTO nodes (
		machine_id,
		kernel,
		architecture,
		gpu_vram_kb,
		gpu_model,
		cpu_model,
		cpu_cores,
		ram_kb,
		ip_addr,
		hostname,
		task_unit,
		node_version
	)
	VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
	)
	ON CONFLICT (machine_id) DO UPDATE SET
		kernel = EXCLUDED.kernel,
		architecture = EXCLUDED.architecture,
		gpu_vram_kb = EXCLUDED.gpu_vram_kb,
		gpu_model = EXCLUDED.gpu_model,
		cpu_model = EXCLUDED.cpu_model,
		cpu_cores = EXCLUDED.cpu_cores,
		ram_kb = EXCLUDED.ram_kb,
		ip_addr = EXCLUDED.ip_addr,
		hostname = EXCLUDED.hostname,
		task_unit = EXCLUDED.task_unit,
		node_version = EXCLUDED.node_version,
		updated_at = now()
	WHERE nodes.status != 'inactive'::node_status
	RETURNING machine_id
	`

	var retId string
	row := s.pool.QueryRow(ctx, query,
		n.MachineID, n.Kernel, n.Architecture,
		n.GPURamKB, n.GPUModel, n.CPUModel, n.CPUCores,
		n.RAMKB, n.IPAddr, n.Hostname, n.TaskUnit,
		n.NodeVersion)

	if err := row.Scan(&retId); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errors.New("registration rejected: node is marked inactive")
		}
		return "", err
	}

	return retId, nil
}

func (s *service) UpdateNodeStatus(ctx context.Context, machineID string, status NodeStatus) (string, error) {
	query := `UPDATE nodes
		SET status = $1, updated_at = now()
		WHERE machine_id = $2
		RETURNING machine_id
	`

	row := s.pool.QueryRow(ctx, query, status, machineID)
	var retId string
	if err := row.Scan(&retId); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	return retId, nil
}

func (s *service) GetNode(ctx context.Context, machineID string) (Node, error) {
	query := `SELECT 
		machine_id,
		kernel,
		architecture,
		gpu_vram_kb,
		gpu_model,
		cpu_model,
		cpu_cores,
		ram_kb,
		ip_addr,
		hostname,
		cloud_region,
		cloud_platform,
		task_unit,
		status,
		node_version,
		last_heartbeat_at,
		registered_at,
		updated_at
	FROM nodes
	WHERE machine_id = $1
	`

	rows, err := s.pool.Query(ctx, query, machineID)
	if err != nil {
		return Node{}, err
	}
	defer rows.Close()
	return pgx.CollectOneRow(rows, pgx.RowToStructByName[Node])
}

func (s *service) GetNodes(ctx context.Context, page int, perPage int) ([]Node, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 10
	}
	offset := (page - 1) * perPage

	query := `SELECT 
		machine_id,
		kernel,
		architecture,
		gpu_vram_kb,
		gpu_model,
		cpu_model,
		cpu_cores,
		ram_kb,
		ip_addr,
		hostname,
		cloud_region,
		cloud_platform,
		task_unit,
		status,
		node_version,
		last_heartbeat_at,
		registered_at,
		updated_at
	FROM nodes
	LIMIT $1 OFFSET $2
	`

	rows, err := s.pool.Query(ctx, query, perPage, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, pgx.RowToStructByName[Node])
}

func (s *service) UpdateNodeLastHBeat(ctx context.Context, machineID string) (string, error) {
	query := `UPDATE nodes
		SET last_heartbeat_at = now(), status = 'active'::node_status
		WHERE machine_id = $1 AND nodes.status != 'inactive'::node_status
		RETURNING machine_id
	`
	var retID string
	row := s.pool.QueryRow(ctx, query, machineID)
	if err := row.Scan(&retID); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}

	return retID, nil
}

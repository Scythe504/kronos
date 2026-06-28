package database

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type TaskType string

const (
	TaskTypeCron        TaskType = "cron"
	TaskTypeOneOff      TaskType = "one_off"
	TaskTypeEventDriven TaskType = "event_driven"
)

type TaskUnit string

const (
	TaskUnitCPU TaskUnit = "cpu"
	TaskUnitGPU TaskUnit = "gpu"
)

type TaskStatus string

const (
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
)

type Task struct {
	ID                       uuid.UUID       `db:"id" json:"id"`
	PayloadSlug              string          `db:"payload_slug" json:"payload_slug"`
	Payload                  json.RawMessage `db:"payload" json:"payload"`
	RetryCount               *int            `db:"retry_count" json:"retry_count"`
	MaxRetryCount            *int            `db:"max_retry_count" json:"max_retry_count"`
	LastError                json.RawMessage `db:"last_error" json:"last_error"`
	ExecutionScheduleTime    *int64          `db:"execution_schedule_time" json:"execution_schedule_time"`
	CronExpression           *string         `db:"cron_expression" json:"cron_expression"`
	ExecutionIntervalSeconds *int64          `db:"execution_interval_seconds" json:"execution_interval_seconds"`
	NextRetryAt              time.Time       `db:"next_retry_at" json:"next_retry_at"`
	TaskType                 TaskType        `db:"task_type" json:"task_type"`
	Status                   TaskStatus      `db:"status" json:"status"`
	AllocatedUnit            TaskUnit        `db:"allocated_unit" json:"allocated_unit"`
	AssignedNodeID           *string         `db:"assigned_node_id" json:"assigned_node_id"`
	CreatedAt                time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt                time.Time       `db:"updated_at" json:"updated_at"`
	DeletedAt                sql.NullTime    `db:"deleted_at" json:"deleted_at"`
}

type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusIdle     NodeStatus = "idle"
	NodeStatusDead     NodeStatus = "dead"
	NodeStatusInactive NodeStatus = "inactive"
)

type Node struct {
	MachineID       string     `db:"machine_id" json:"machine_id"`
	Kernel          string     `db:"kernel" json:"kernel"`
	Architecture    string     `db:"architecture" json:"architecture"`
	GPURamKB        *int64     `db:"gpu_vram_kb" json:"gpu_vram_kb"`
	GPUModel        *string    `db:"gpu_model" json:"gpu_model"`
	CPUModel        string     `db:"cpu_model" json:"cpu_model"`
	CPUCores        int        `db:"cpu_cores" json:"cpu_cores"`
	RAMKB           int64      `db:"ram_kb" json:"ram_kb"`
	IPAddr          string     `db:"ip_addr" json:"ip_addr"`
	Hostname        string     `db:"hostname" json:"hostname"`
	CloudRegion     *string    `db:"cloud_region" json:"cloud_region"`
	CloudPlatform   *string    `db:"cloud_platform" json:"cloud_platform"`
	TaskUnit        TaskUnit   `db:"task_unit" json:"task_unit"`
	Status          NodeStatus `db:"status" json:"status"`
	NodeVersion     string     `db:"node_version" json:"node_version"`
	LastHeartbeatAt time.Time  `db:"last_heartbeat_at" json:"last_heartbeat_at"`
	RegisteredAt    time.Time  `db:"registered_at" json:"registered_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}

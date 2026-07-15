package database

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// TaskUnit represents the hardware target of a task (e.g., cpu, gpu)
type TaskUnit string

const (
	TaskUnitCPU TaskUnit = "cpu"
	TaskUnitGPU TaskUnit = "gpu"
)

// TaskStatus represents the current state of a task
type TaskStatus string

const (
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusQueued    TaskStatus = "queued"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusPending   TaskStatus = "pending"
)

// NodeStatus represents the current state of a node
type NodeStatus string

const (
	NodeStatusActive   NodeStatus = "active"
	NodeStatusIdle     NodeStatus = "idle"
	NodeStatusDead     NodeStatus = "dead"
	NodeStatusInactive NodeStatus = "inactive"
)

// TriggerType represents how a workflow is initiated
type TriggerType string

const (
	TriggerTypeCron    TriggerType = "cron"
	TriggerTypeWebhook TriggerType = "webhook"
)

// TriggerCondition represents the condition for continuing step or chain execution
type TriggerCondition string

const (
	TriggerConditionOnSuccess TriggerCondition = "on_success"
	TriggerConditionOnFailure TriggerCondition = "on_failure"
)

// Worker represents the configuration of a registered worker service/executable
type Worker struct {
	Slug               string    `db:"slug" json:"slug"`
	Name               string    `db:"name" json:"name"`
	Description        *string   `db:"description" json:"description"`
	RepoURL            *string   `db:"repo_url" json:"repo_url"`
	RepoRef            *string   `db:"repo_ref" json:"repo_ref"`
	EnvVars            []byte    `db:"env_vars" json:"env_vars"`
	Entrypoint         string    `db:"entrypoint" json:"entrypoint"`
	TaskUnit           TaskUnit  `db:"task_unit" json:"task_unit"`
	TaskTimeoutSeconds int       `db:"task_timeout_seconds" json:"task_timeout_seconds"`
	CreatedAt          time.Time `db:"created_at" json:"created_at"`
	UpdatedAt          time.Time `db:"updated_at" json:"updated_at"`
}

// Node represents a compute node registered with the cluster
type Node struct {
	ID              *uuid.UUID `db:"id" json:"id"`
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
	CloudRegion     string     `db:"cloud_region" json:"cloud_region"`
	CloudPlatform   string     `db:"cloud_platform" json:"cloud_platform"`
	TaskUnit        TaskUnit   `db:"task_unit" json:"task_unit"`
	Status          NodeStatus `db:"status" json:"status"`
	NodeVersion     string     `db:"node_version" json:"node_version"`
	LastHeartbeatAt time.Time  `db:"last_heartbeat_at" json:"last_heartbeat_at"`
	RegisteredAt    time.Time  `db:"registered_at" json:"registered_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}

// Task represents an executable unit of work dispatched to a node
type Task struct {
	ID             uuid.UUID       `db:"id" json:"id"`
	WorkflowRunID  *uuid.UUID      `db:"workflow_run_id" json:"workflow_run_id"`
	WorkflowStepID *uuid.UUID      `db:"workflow_step_id" json:"workflow_step_id"`
	WorkflowID     *uuid.UUID      `db:"workflow_id" json:"workflow_id"`
	PayloadSlug    string          `db:"payload_slug" json:"payload_slug"`
	Payload        json.RawMessage `db:"payload" json:"payload"`
	RetryCount     *int            `db:"retry_count" json:"retry_count"`
	MaxRetryCount  *int            `db:"max_retry_count" json:"max_retry_count"`
	LastError      json.RawMessage `db:"last_error" json:"last_error"`
	NextRetryAt    sql.NullTime    `db:"next_retry_at" json:"next_retry_at"`
	Status         TaskStatus      `db:"status" json:"status"`
	AllocatedUnit  TaskUnit        `db:"allocated_unit" json:"allocated_unit"`
	AssignedNodeID *string         `db:"assigned_node_id" json:"assigned_node_id"`
	ChainTask      bool            `db:"chain_task" json:"chain_task"`
	CreatedAt      time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time       `db:"updated_at" json:"updated_at"`
	DeletedAt      sql.NullTime    `db:"deleted_at" json:"deleted_at"`
}

// Workflow represents a defined pipeline of task executions
type Workflow struct {
	ID            uuid.UUID       `db:"id" json:"id"`
	Name          string          `db:"name" json:"name"`
	TriggerType   TriggerType     `db:"trigger_type" json:"trigger_type"`
	TriggerConfig json.RawMessage `db:"trigger_config" json:"trigger_config"`
	NextRunAt     sql.NullTime    `db:"next_run_at" json:"next_run_at"`
	CreatedAt     time.Time       `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time       `db:"updated_at" json:"updated_at"`
	DeletedAt     sql.NullTime    `db:"deleted_at" json:"deleted_at"`
}

// WorkflowRun represents a defined instance of a workflow execution
type WorkflowRun struct {
	ID         uuid.UUID `db:"id" json:"id"`
	WorkflowID uuid.UUID `db:"workflow_id" json:"workflow_id"`
	Status     string    `db:"status" json:"status"`
	CreatedAt  time.Time `db:"created_at" json:"created_at"`
	UpdatedAt  time.Time `db:"updated_at" json:"updated_at"`
}

// WorkflowStep represents a step in a workflow
type WorkflowStep struct {
	ID         uuid.UUID        `db:"id" json:"id"`
	WorkflowID uuid.UUID        `db:"workflow_id" json:"workflow_id"`
	Slug       string           `db:"slug" json:"slug"`
	Condition  TriggerCondition `db:"condition" json:"condition"`
	StepOrder  int              `db:"step_order" json:"step_order"`
	Payload    json.RawMessage  `db:"payload" json:"payload"`
}

// TaskChain represents follow-on execution links between tasks
type TaskChain struct {
	ID               uuid.UUID        `db:"id" json:"id"`
	TriggerTaskID    uuid.UUID        `db:"trigger_task_id" json:"trigger_task_id"`
	FollowOnTaskID   uuid.UUID        `db:"follow_on_task_id" json:"follow_on_task_id"`
	TriggererPayload json.RawMessage  `db:"triggerer_payload" json:"triggerer_payload"`
	Condition        TriggerCondition `db:"condition" json:"condition"`
}

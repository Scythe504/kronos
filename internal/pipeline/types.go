package pipeline

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type WorkerResultMessage string

const (
	WorkerResultSuccessMesssage   WorkerResultMessage = "success"
	WorkerResultFailedMessage     WorkerResultMessage = "failed"
	WorkerResultACKMessage        WorkerResultMessage = "ack"
	WorkerResultACKTimeoutMessage WorkerResultMessage = "ack_timeout"
)

type WorkerResult struct {
	TaskID        uuid.UUID           `json:"task_id"`
	ResultMessage WorkerResultMessage `json:"result_message"`
	Output        json.RawMessage     `json:"output,omitempty"`
	Error         json.RawMessage     `json:"error,omitempty"`
	Timestamp     time.Time           `json:"timestamp"`
}

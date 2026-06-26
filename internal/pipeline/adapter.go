package pipeline

import (
	"encoding/json"

	"github.com/scythe504/fluxd/internal/database"
)

type WorkerPayload struct {
	TaskID string `json:"task_id"`
	Slug string `json:"slug"`
	Payload json.RawMessage `json:"payload"`
}

func AdaptTask(task database.Task) ([]byte, error) {
	wp := WorkerPayload{
		TaskID: task.ID.String(),
		Slug: task.PayloadSlug,
		Payload: task.Payload,
	}

	data, err := json.Marshal(wp)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

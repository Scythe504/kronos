package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"sync"
	"time"
)

type WorkerPayload struct {
	TaskID  string          `json:"task_id"`
	Slug    string          `json:"slug"`
	Payload json.RawMessage `json:"payload"`
}

type CsvPayload struct {
	SourceURI  string `json:"source_uri"`
	TargetURI  string `json:"target_uri"`
	Layout     string `json:"layout"`
	HasHeaders bool   `json:"has_headers"`
	FontSize   int    `json:"font_size"`
}

type WorkerResultMessage string

const (
	WorkerResultSuccessMesssage WorkerResultMessage = "success"
	WorkerResultFailedMessage   WorkerResultMessage = "failed"
	WorkerResultACKMessage      WorkerResultMessage = "ack"
)

type WorkerResult struct {
	TaskID        string              `json:"task_id"`
	ResultMessage WorkerResultMessage `json:"result_message"`
	Error         json.RawMessage     `json:"error,omitempty"`
	Timestamp     time.Time           `json:"timestamp,omitempty"`
}

var stdoutMu sync.Mutex

func writeResult(res WorkerResult) {
	stdoutMu.Lock()
	defer stdoutMu.Unlock()
	_ = json.NewEncoder(os.Stdout).Encode(res)
}

func main() {
	const numWorkers = 10
	taskCh := make(chan WorkerPayload, 100)
	var wg sync.WaitGroup

	// Start worker pool
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for wp := range taskCh {
				processTask(wp)
			}
		}()
	}

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var wp WorkerPayload
		if err := json.Unmarshal(line, &wp); err != nil {
			log.Printf("Failed to decode worker payload wrapper: %v", err)
			continue
		}

		// 1. Immediately write ACK back to stdout
		writeResult(WorkerResult{
			TaskID:        wp.TaskID,
			ResultMessage: WorkerResultACKMessage,
			Timestamp:     time.Now(),
		})

		// 2. Queue task to worker pool
		taskCh <- wp
	}

	close(taskCh)
	wg.Wait()

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading stdin: %v", err)
	}
}

func processTask(wp WorkerPayload) {
	var payload CsvPayload
	if err := json.Unmarshal(wp.Payload, &payload); err != nil {
		log.Printf("Failed to decode csv payload: %v", err)
		writeError(wp.TaskID, err.Error())
		return
	}

	writeResult(WorkerResult{
		TaskID:        wp.TaskID,
		ResultMessage: WorkerResultSuccessMesssage,
		Timestamp:     time.Now(),
	})
}

func writeError(taskID string, errMsg string) {
	errBytes, _ := json.Marshal(errMsg)
	writeResult(WorkerResult{
		TaskID:        taskID,
		ResultMessage: WorkerResultFailedMessage,
		Error:         errBytes,
		Timestamp:     time.Now(),
	})
}

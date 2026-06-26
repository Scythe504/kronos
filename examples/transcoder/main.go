package main

import (
	"bufio"
	"encoding/json"
	"log"
	"os"
	"time"
)

type WorkerPayload struct {
	TaskID  string          `json:"task_id"`
	Slug    string          `json:"slug"`
	Payload json.RawMessage `json:"payload"`
}

type VideoPayload struct {
	SourceURI    string `json:"source_uri"`
	TargetURI    string `json:"target_uri"`
	Resolution   string `json:"resolution"`
	Format       string `json:"format"`
	ExtractAudio bool   `json:"extract_audio"`
}

type WorkerResult struct {
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	BytesWritten int    `json:"bytes_written"`
	TimeTakenMs  int64  `json:"time_taken_ms"`
	Error        string `json:"error,omitempty"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		start := time.Now()

		var wp WorkerPayload
		if err := json.Unmarshal(line, &wp); err != nil {
			log.Printf("Failed to decode worker payload wrapper: %v", err)
			continue
		}

		var payload VideoPayload
		if err := json.Unmarshal(wp.Payload, &payload); err != nil {
			log.Printf("Failed to decode video payload: %v", err)
			writeError(wp.TaskID, err.Error(), time.Since(start).Milliseconds())
			continue
		}

		// Simulate Work (e.g., FFMPEG processing)
		time.Sleep(2 * time.Second) // Fake processing time

		result := WorkerResult{
			TaskID:       wp.TaskID,
			Status:       "success",
			BytesWritten: 10485760, // 10MB simulated
			TimeTakenMs:  time.Since(start).Milliseconds(),
		}

		if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
			log.Printf("Failed to write result to stdout: %v", err)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Fatalf("Error reading stdin: %v", err)
	}
}

func writeError(taskID string, errMsg string, elapsed int64) {
	result := WorkerResult{
		TaskID:       taskID,
		Status:       "failed",
		Error:        errMsg,
		TimeTakenMs:  elapsed,
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
}

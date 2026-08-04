package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
)

type createTaskRequest struct {
	PayloadSlug    string          `json:"payload_slug"`
	Payload        json.RawMessage `json:"payload"`
	AllocatedUnit  string          `json:"allocated_unit,omitempty"`
	WorkflowRunID  string          `json:"workflow_run_id,omitempty"`
	WorkflowStepID string          `json:"workflow_step_id,omitempty"`
	WorkflowID     string          `json:"workflow_id,omitempty"`
	ChainTask      bool            `json:"chain_task,omitempty"`
}

type taskResponse struct {
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
	Count   int64  `json:"count,omitempty"`
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	var reqBody createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to decode create task body", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	if reqBody.PayloadSlug == "" {
		utils.WriteError(w, http.StatusBadRequest, "payload_slug is required")
		return
	}

	var unit *database.TaskUnit
	if reqBody.AllocatedUnit != "" {
		u := database.TaskUnit(reqBody.AllocatedUnit)
		unit = &u
	}

	var runID *uuid.UUID
	if reqBody.WorkflowRunID != "" {
		if id, err := uuid.Parse(reqBody.WorkflowRunID); err == nil {
			runID = &id
		}
	}

	var stepID *uuid.UUID
	if reqBody.WorkflowStepID != "" {
		if id, err := uuid.Parse(reqBody.WorkflowStepID); err == nil {
			stepID = &id
		}
	}

	var workflowID *uuid.UUID
	if reqBody.WorkflowID != "" {
		if id, err := uuid.Parse(reqBody.WorkflowID); err == nil {
			workflowID = &id
		}
	}

	taskID, err := s.db.CreateTask(r.Context(), reqBody.PayloadSlug, reqBody.Payload, runID, stepID, workflowID, unit, reqBody.ChainTask)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to create task", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create task")
		return
	}

	s.tel.LogInfo(r.Context(), "Task created successfully", "task_id", taskID.String(), "slug", reqBody.PayloadSlug)
	utils.WriteJSON(w, http.StatusCreated, taskResponse{
		Message: "Task created successfully",
		ID:      taskID.String(),
	})
}

func (s *Server) createTasksBulk(w http.ResponseWriter, r *http.Request) {
	contentType := r.Header.Get("Content-Type")

	var tasks []database.Task

	if strings.Contains(contentType, "application/x-ndjson") {
		scanner := bufio.NewScanner(r.Body)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var req createTaskRequest
			if err := json.Unmarshal(line, &req); err == nil && req.PayloadSlug != "" {
				unit := database.TaskUnit(req.AllocatedUnit)
				if unit == "" {
					unit = database.TaskUnitCPU
				}
				tasks = append(tasks, database.Task{
					PayloadSlug:   req.PayloadSlug,
					Payload:       req.Payload,
					AllocatedUnit: unit,
					Status:        database.TaskStatusQueued,
				})
			}
		}
		if err := scanner.Err(); err != nil {
			s.tel.LogInfo(r.Context(), "Error scanning NDJSON stream", "path", r.URL.Path, "error", err)
			utils.WriteError(w, http.StatusBadRequest, "Error reading NDJSON stream")
			return
		}
	} else {
		var reqs []createTaskRequest
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
			return
		}
		defer r.Body.Close()

		if err := json.Unmarshal(bodyBytes, &reqs); err != nil {
			var singleReq createTaskRequest
			if err := json.Unmarshal(bodyBytes, &singleReq); err == nil {
				reqs = append(reqs, singleReq)
			} else {
				utils.WriteError(w, http.StatusBadRequest, "Invalid JSON Array/Object")
				return
			}
		}

		for _, req := range reqs {
			unit := database.TaskUnit(req.AllocatedUnit)
			if unit == "" {
				unit = database.TaskUnitCPU
			}
			tasks = append(tasks, database.Task{
				PayloadSlug:   req.PayloadSlug,
				Payload:       req.Payload,
				AllocatedUnit: unit,
				Status:        database.TaskStatusQueued,
			})
		}
	}

	if len(tasks) == 0 {
		utils.WriteError(w, http.StatusBadRequest, "No valid tasks provided")
		return
	}

	if err := s.db.CreateTasks(r.Context(), nil, tasks); err != nil {
		s.tel.LogErrorln(r.Context(), "Failed bulk task creation", "count", len(tasks), "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to submit bulk tasks")
		return
	}

	utils.WriteJSON(w, http.StatusCreated, taskResponse{
		Message: fmt.Sprintf("%d tasks submitted successfully", len(tasks)),
		Count:   int64(len(tasks)),
	})
}

func (s *Server) getTasks(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("per_page")
	status := r.URL.Query().Get("status")
	slug := r.URL.Query().Get("slug")

	page := 1
	perPage := 10
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 {
		perPage = pp
	}

	tasks, err := s.db.ListTasks(r.Context(), page, perPage, status, slug)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to list tasks", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list tasks")
		return
	}

	utils.WriteJSON(w, http.StatusOK, tasks)
}

func (s *Server) getTaskByID(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		utils.WriteError(w, http.StatusBadRequest, "Task ID parameter required")
		return
	}

	task, err := s.db.GetTask(r.Context(), id)
	if err != nil {
		s.tel.LogInfo(r.Context(), "Task not found", "id", id, "path", r.URL.Path, "httpCode", http.StatusNotFound, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Task '%s' not found", id))
		return
	}

	utils.WriteJSON(w, http.StatusOK, task)
}

func (s *Server) retryTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		TaskIDs []string `json:"task_ids,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	defer r.Body.Close()

	affected, err := s.db.RetryFailedTasks(r.Context(), body.TaskIDs)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to retry tasks", "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to retry tasks")
		return
	}

	utils.WriteJSON(w, http.StatusOK, taskResponse{
		Message: fmt.Sprintf("%d failed tasks requeued", affected),
		Count:   affected,
	})
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		utils.WriteError(w, http.StatusBadRequest, "Task ID parameter required")
		return
	}

	deletedID, err := s.db.DeleteTask(r.Context(), id)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to delete task", "id", id, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Task '%s' not found or already deleted", id))
		return
	}

	utils.WriteJSON(w, http.StatusOK, taskResponse{
		Message: fmt.Sprintf("Task '%s' deleted successfully", deletedID),
		ID:      deletedID,
	})
}
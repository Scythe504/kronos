package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
)

type workflowResponse struct {
	Message string `json:"message"`
	ID      string `json:"id,omitempty"`
	RunID   string `json:"run_id,omitempty"`
}

func validateSequentialSteps(steps []database.Step) error {
	if len(steps) == 0 {
		return fmt.Errorf("steps cannot be empty")
	}

	seenOrders := make(map[int]bool, len(steps))
	for _, step := range steps {
		if step.StepOrder < 1 {
			return fmt.Errorf("invalid step_order %d: step orders must be >= 1", step.StepOrder)
		}
		if seenOrders[step.StepOrder] {
			return fmt.Errorf("duplicate step_order %d detected", step.StepOrder)
		}
		seenOrders[step.StepOrder] = true
	}

	for i := 1; i <= len(steps); i++ {
		if !seenOrders[i] {
			return fmt.Errorf("missing step_order %d: step orders must be strictly sequential from 1 to %d without gaps", i, len(steps))
		}
	}

	return nil
}

func (s *Server) createWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	var payload database.WorkflowPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to decode workflow template payload", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	if payload.Name == "" {
		utils.WriteError(w, http.StatusBadRequest, "Workflow name/slug is required")
		return
	}

	if err := validateSequentialSteps(payload.Steps); err != nil {
		s.tel.LogInfo(r.Context(), "Invalid workflow steps sequence", "name", payload.Name, "error", err)
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	workflowID, err := s.db.CreateWorkflowTemplate(r.Context(), payload)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to create workflow template", "name", payload.Name, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create workflow template")
		return
	}

	s.tel.LogInfo(r.Context(), "Workflow template created successfully", "workflow_id", workflowID.String(), "name", payload.Name)
	utils.WriteJSON(w, http.StatusCreated, workflowResponse{
		Message: fmt.Sprintf("Workflow template '%s' created successfully", payload.Name),
		ID:      workflowID.String(),
	})
}

func (s *Server) getWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	perPageStr := r.URL.Query().Get("per_page")

	page := 1
	perPage := 10
	if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
		page = p
	}
	if pp, err := strconv.Atoi(perPageStr); err == nil && pp > 0 {
		perPage = pp
	}

	workflows, err := s.db.GetWorkflowTemplates(r.Context(), page, perPage)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to list workflow templates", "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list workflow templates")
		return
	}

	utils.WriteJSON(w, http.StatusOK, workflows)
}

func (s *Server) getWorkflowTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		utils.WriteError(w, http.StatusBadRequest, "Workflow ID parameter required")
		return
	}

	wf, err := s.db.GetWorkflowTemplate(r.Context(), id)
	if err != nil {
		s.tel.LogInfo(r.Context(), "Workflow template not found", "id", id, "httpCode", http.StatusNotFound, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Workflow template '%s' not found", id))
		return
	}

	utils.WriteJSON(w, http.StatusOK, wf)
}

func (s *Server) triggerWorkflow(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		idStr = chi.URLParam(r, "workflow_id")
	}
	if idStr == "" {
		utils.WriteError(w, http.StatusBadRequest, "Workflow ID parameter required")
		return
	}

	workflowUUID, err := uuid.Parse(idStr)
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid Workflow ID format")
		return
	}

	runID, err := s.db.TriggerWorkflow(r.Context(), workflowUUID)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to trigger workflow", "id", idStr, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to trigger workflow")
		return
	}

	s.tel.LogInfo(r.Context(), "Workflow triggered successfully", "workflow_id", idStr, "run_id", runID.String())
	utils.WriteJSON(w, http.StatusOK, workflowResponse{
		Message: "Workflow triggered successfully",
		ID:      idStr,
		RunID:   runID.String(),
	})
}

func (s *Server) createTaskchain(w http.ResponseWriter, r *http.Request) {
	var steps []database.Step
	if err := json.NewDecoder(r.Body).Decode(&steps); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to decode task chain steps", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	if err := validateSequentialSteps(steps); err != nil {
		s.tel.LogInfo(r.Context(), "Invalid task chain steps sequence", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}

	taskIDs, err := s.db.CreateTaskChain(r.Context(), steps)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to create task chain", "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create task chain")
		return
	}

	utils.WriteJSON(w, http.StatusCreated, map[string]any{
		"message":  "Task chain created successfully",
		"task_ids": taskIDs,
	})
}

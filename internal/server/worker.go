package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/scythe504/kronos/internal/builder"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
)

type workerRequest struct {
	Slug               string `json:"slug"`
	Description        string `json:"description,omitempty"`
	Name               string `json:"name,omitempty"`
	RepoUrl            string `json:"repo_url"`
	RepoRef            string `json:"repo_ref,omitempty"`
	EnvVars            string `json:"env,omitempty"`
	EnvVarsAlt         string `json:"env_vars,omitempty"`
	PreBuildCommand    string `json:"pre_build_command,omitempty"`
	BuildCommand       string `json:"build_command,omitempty"`
	RunCommand         string `json:"run_command,omitempty"`
	DockerfilePath     string `json:"dockerfile_path,omitempty"`
	Entrypoint         string `json:"entrypoint,omitempty"`
	TaskUnit           string `json:"task_unit,omitempty"`
	TaskTimeoutSeconds int64  `json:"task_timeout_seconds,omitempty"`
}

type workerResponse struct {
	Message string `json:"message"`
}

func (s *Server) createWorker(w http.ResponseWriter, r *http.Request) {
	var reqBody workerRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to read req body", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	repoRef, err := builder.GetRepoRef(r.Context(), reqBody.RepoUrl)
	if err != nil {
		s.tel.LogInfo(r.Context(), "Failed to validate repoUrl", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Repo Url")
		return
	}
	if reqBody.RepoRef == "" {
		reqBody.RepoRef = repoRef
	}

	taskTimeout := reqBody.TaskTimeoutSeconds
	if taskTimeout == 0 {
		taskTimeout = 300
	}
	taskUnit := database.TaskUnit(reqBody.TaskUnit)
	if taskUnit == "" {
		taskUnit = database.TaskUnitCPU
	}

	var preBuildCmd *string
	if reqBody.PreBuildCommand != "" {
		preBuildCmd = &reqBody.PreBuildCommand
	}
	var buildCmd *string
	if reqBody.BuildCommand != "" {
		buildCmd = &reqBody.BuildCommand
	}
	var runCmd *string
	if reqBody.RunCommand != "" {
		runCmd = &reqBody.RunCommand
	}
	var dockerfilePath *string
	if reqBody.DockerfilePath != "" {
		dockerfilePath = &reqBody.DockerfilePath
	}

	rawEnv := reqBody.EnvVars
	if rawEnv == "" {
		rawEnv = reqBody.EnvVarsAlt
	}
	envBytes := []byte(rawEnv)
	if len(envBytes) > 0 {
		// If provided as KEY=VAL plaintext lines, convert to JSON object bytes
		var testMap map[string]string
		if err := json.Unmarshal(envBytes, &testMap); err != nil {
			envMap := make(map[string]string)
			for line := range strings.SplitSeq(string(envBytes), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					envMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
			if converted, err := json.Marshal(envMap); err == nil {
				envBytes = converted
			}
		}
	}

	worker := database.Worker{
		Slug:               reqBody.Slug,
		Name:               reqBody.Name,
		Description:        &reqBody.Description,
		RepoURL:            reqBody.RepoUrl,
		RepoRef:            reqBody.RepoRef,
		EnvVars:            envBytes,
		PreBuildCommand:    preBuildCmd,
		BuildCommand:       buildCmd,
		RunCommand:         runCmd,
		DockerfilePath:     dockerfilePath,
		Entrypoint:         reqBody.Entrypoint,
		TaskUnit:           taskUnit,
		TaskTimeoutSeconds: int(taskTimeout),
	}

	slug, err := s.db.UpsertWorker(r.Context(), nil, []database.Worker{worker})
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to upsert worker", "path", r.URL.Path, "httpCode", http.StatusInternalServerError, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create worker")
		return
	}

	utils.WriteJSON(w, http.StatusCreated, workerResponse{Message: fmt.Sprintf("Worker '%s' created successfully", slug)})
}

func (s *Server) getWorker(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		s.tel.LogInfo(r.Context(), "Missing worker slug in URL", "path", r.URL.Path, "httpCode", http.StatusBadRequest)
		utils.WriteError(w, http.StatusBadRequest, "Worker slug parameter required")
		return
	}

	worker, err := s.db.GetWorker(r.Context(), slug)
	if err != nil {
		s.tel.LogInfo(r.Context(), "Worker not found", "slug", slug, "path", r.URL.Path, "httpCode", http.StatusNotFound, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Worker '%s' not found", slug))
		return
	}

	utils.WriteJSON(w, http.StatusOK, worker)
}

func (s *Server) getWorkers(w http.ResponseWriter, r *http.Request) {
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

	workers, err := s.db.GetWorkers(r.Context(), page, perPage)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to list workers", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list workers")
		return
	}

	utils.WriteJSON(w, http.StatusOK, workers)
}

func (s *Server) updateWorker(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		s.tel.LogInfo(r.Context(), "Missing worker slug in URL", "path", r.URL.Path, "httpCode", http.StatusBadRequest)
		utils.WriteError(w, http.StatusBadRequest, "Worker slug parameter required")
		return
	}

	var reqBody workerRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to read req body", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	reqBody.Slug = slug

	if reqBody.RepoUrl != "" && reqBody.RepoRef == "" {
		ref, err := builder.GetRepoRef(r.Context(), reqBody.RepoUrl)
		if err == nil {
			reqBody.RepoRef = ref
		}
	}

	taskTimeout := reqBody.TaskTimeoutSeconds
	if taskTimeout == 0 {
		taskTimeout = 300
	}
	taskUnit := database.TaskUnit(reqBody.TaskUnit)
	if taskUnit == "" {
		taskUnit = database.TaskUnitCPU
	}

	var preBuildCmd *string
	if reqBody.PreBuildCommand != "" {
		preBuildCmd = &reqBody.PreBuildCommand
	}
	var buildCmd *string
	if reqBody.BuildCommand != "" {
		buildCmd = &reqBody.BuildCommand
	}
	var runCmd *string
	if reqBody.RunCommand != "" {
		runCmd = &reqBody.RunCommand
	}
	var dockerfilePath *string
	if reqBody.DockerfilePath != "" {
		dockerfilePath = &reqBody.DockerfilePath
	}

	envBytes := []byte(reqBody.EnvVars)
	if len(envBytes) > 0 {
		var testMap map[string]string
		if err := json.Unmarshal(envBytes, &testMap); err != nil {
			envMap := make(map[string]string)
			for line := range strings.SplitSeq(string(envBytes), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.HasPrefix(line, "#") {
					continue
				}
				parts := strings.SplitN(line, "=", 2)
				if len(parts) == 2 {
					envMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
				}
			}
			if converted, err := json.Marshal(envMap); err == nil {
				envBytes = converted
			}
		}
	}

	worker := database.Worker{
		Slug:               reqBody.Slug,
		Name:               reqBody.Name,
		Description:        &reqBody.Description,
		RepoURL:            reqBody.RepoUrl,
		RepoRef:            reqBody.RepoRef,
		EnvVars:            envBytes,
		PreBuildCommand:    preBuildCmd,
		BuildCommand:       buildCmd,
		RunCommand:         runCmd,
		DockerfilePath:     dockerfilePath,
		Entrypoint:         reqBody.Entrypoint,
		TaskUnit:           taskUnit,
		TaskTimeoutSeconds: int(taskTimeout),
	}

	updatedSlug, err := s.db.UpsertWorker(r.Context(), nil, []database.Worker{worker})
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to update worker", "slug", slug, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to update worker")
		return
	}

	utils.WriteJSON(w, http.StatusOK, workerResponse{Message: fmt.Sprintf("Worker '%s' updated successfully", updatedSlug)})
}

func (s *Server) deleteWorker(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	if slug == "" {
		s.tel.LogInfo(r.Context(), "Missing worker slug in URL", "path", r.URL.Path, "httpCode", http.StatusBadRequest)
		utils.WriteError(w, http.StatusBadRequest, "Worker slug parameter required")
		return
	}

	deletedSlug, err := s.db.DeleteWorker(r.Context(), slug)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to delete worker", "slug", slug, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Worker '%s' not found or already deleted", slug))
		return
	}

	s.tel.LogInfo(r.Context(), "Worker deleted successfully", "slug", deletedSlug)
	utils.WriteJSON(w, http.StatusOK, workerResponse{Message: fmt.Sprintf("Worker '%s' deleted successfully", deletedSlug)})
}

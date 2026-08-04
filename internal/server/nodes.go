package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/scythe504/kronos/internal/database"
	"github.com/scythe504/kronos/internal/utils"
)

type nodeInitRequest struct {
	NodeID       string   `json:"node_id,omitempty"`
	MachineID    string   `json:"machine_id"`
	Secret       string   `json:"secret"`
	TaskUnit     string   `json:"task_unit"`
	AllowedSlugs []string `json:"allowed_slugs,omitempty"`

	Hostname     string `json:"hostname"`
	IPAddr       string `json:"ip_addr"`
	CPUModel     string `json:"cpu_model"`
	CPUCores     int    `json:"cpu_cores"`
	RAMKB        int64  `json:"ram_kb"`
	GPUModel     string `json:"gpu_model,omitempty"`
	GPUVRAMKB    int64  `json:"gpu_vram_kb,omitempty"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	NodeVersion  string `json:"node_version,omitempty"`
}

type nodeInitResponse struct {
	NodeID       string   `json:"node_id"`
	DBURL        string   `json:"db_url"`
	AllowedSlugs []string `json:"allowed_slugs"`
	TaskUnit     string   `json:"task_unit"`
}

type nodeResponse struct {
	Message string `json:"message"`
}

func (s *Server) initNode(w http.ResponseWriter, r *http.Request) {
	var reqBody nodeInitRequest
	if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
		s.tel.LogInfo(r.Context(), "Failed to decode node init body", "path", r.URL.Path, "httpCode", http.StatusBadRequest, "error", err)
		utils.WriteError(w, http.StatusBadRequest, "Invalid Request Body")
		return
	}
	defer r.Body.Close()

	expectedSecret := os.Getenv("AGENT_SECRET")
	if expectedSecret == "" {
		expectedSecret = os.Getenv("NODE_SECRET")
	}
	if expectedSecret != "" && reqBody.Secret != expectedSecret {
		s.tel.LogInfo(r.Context(), "Node init secret mismatch", "path", r.URL.Path, "httpCode", http.StatusUnauthorized)
		utils.WriteError(w, http.StatusUnauthorized, "Invalid Node Secret")
		return
	}

	unit := database.TaskUnit(reqBody.TaskUnit)
	if unit == "" {
		unit = database.TaskUnitCPU
	}

	allowedSlugs := reqBody.AllowedSlugs
	if len(allowedSlugs) == 0 {
		workers, err := s.db.GetWorkers(r.Context(), 1, 100)
		if err == nil {
			for _, worker := range workers {
				if worker.TaskUnit == unit {
					allowedSlugs = append(allowedSlugs, worker.Slug)
				}
			}
		}
	}

	var parsedUUID *uuid.UUID
	if reqBody.NodeID != "" {
		if u, err := uuid.Parse(reqBody.NodeID); err == nil && u != uuid.Nil {
			parsedUUID = &u
		}
	}

	var gpuModel *string
	if reqBody.GPUModel != "" {
		gpuModel = &reqBody.GPUModel
	}

	var gpuRAM *int64
	if reqBody.GPUVRAMKB > 0 {
		gpuRAM = &reqBody.GPUVRAMKB
	}

	node := database.Node{
		ID:           parsedUUID,
		MachineID:    reqBody.MachineID,
		Kernel:       reqBody.Kernel,
		Architecture: reqBody.Architecture,
		CPUModel:     reqBody.CPUModel,
		CPUCores:     reqBody.CPUCores,
		RAMKB:        reqBody.RAMKB,
		GPUModel:     gpuModel,
		GPURamKB:     gpuRAM,
		IPAddr:       reqBody.IPAddr,
		Hostname:     reqBody.Hostname,
		TaskUnit:     unit,
		NodeVersion:  reqBody.NodeVersion,
	}

	registeredID, err := s.db.RegisterNode(r.Context(), node)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to register node", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to register node")
		return
	}

	dbURL := os.Getenv("DB_URL")

	s.tel.LogInfo(r.Context(), "Node registered successfully", "node_id", registeredID, "machine_id", reqBody.MachineID)

	utils.WriteJSON(w, http.StatusOK, nodeInitResponse{
		NodeID:       registeredID,
		DBURL:        dbURL,
		AllowedSlugs: allowedSlugs,
		TaskUnit:     string(unit),
	})
}

func (s *Server) getNodes(w http.ResponseWriter, r *http.Request) {
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

	nodes, err := s.db.GetNodes(r.Context(), page, perPage)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Failed to list nodes", "path", r.URL.Path, "error", err)
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list nodes")
		return
	}

	utils.WriteJSON(w, http.StatusOK, nodes)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		utils.WriteError(w, http.StatusBadRequest, "Node ID parameter required")
		return
	}

	node, err := s.db.GetNode(r.Context(), id)
	if err != nil {
		s.tel.LogInfo(r.Context(), "Node not found", "id", id, "path", r.URL.Path, "httpCode", http.StatusNotFound, "error", err)
		utils.WriteError(w, http.StatusNotFound, fmt.Sprintf("Node '%s' not found", id))
		return
	}

	utils.WriteJSON(w, http.StatusOK, node)
}

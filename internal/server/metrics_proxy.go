package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/scythe504/kronos/internal/utils"
)

const (
	maxQueryLength     = 512
	maxRangeSeconds    = 30 * 24 * 3600 // 30 days in seconds
	maxResponseBodyBytes = 2 * 1024 * 1024 // 2MB max response
	proxyTimeoutSec    = 5
)

// secureHttpClient creates a dedicated http.Client that does NOT follow redirects
var proxyHttpClient = &http.Client{
	Timeout: proxyTimeoutSec * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// queryPrometheusProxy handles read-only instant PromQL queries (/api/v1/query)
func (s *Server) queryPrometheusProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	query := r.URL.Query().Get("query")
	if strings.TrimSpace(query) == "" {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}

	if len(query) > maxQueryLength {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query string exceeds max length of 512 characters"})
		return
	}

	s.forwardPrometheusRequest(w, r, "/api/v1/query")
}

// queryPrometheusRangeProxy handles read-only range PromQL queries (/api/v1/query_range)
// with a strict maximum time window cap of 30 days.
func (s *Server) queryPrometheusRangeProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		utils.WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "Method not allowed"})
		return
	}

	queryParams := r.URL.Query()
	query := queryParams.Get("query")
	if strings.TrimSpace(query) == "" {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query parameter is required"})
		return
	}

	if len(query) > maxQueryLength {
		utils.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "query string exceeds max length of 512 characters"})
		return
	}

	// Range Enforcement: Max 30 days window
	startStr := queryParams.Get("start")
	endStr := queryParams.Get("end")

	if startStr != "" && endStr != "" {
		start, errStart := strconv.ParseFloat(startStr, 64)
		end, errEnd := strconv.ParseFloat(endStr, 64)

		if errStart == nil && errEnd == nil {
			if (end - start) > float64(maxRangeSeconds) {
				utils.WriteJSON(w, http.StatusBadRequest, map[string]string{
					"error": fmt.Sprintf("Query range cannot exceed 30 days (%d seconds)", maxRangeSeconds),
				})
				return
			}
		}
	}

	s.forwardPrometheusRequest(w, r, "/api/v1/query_range")
}

func (s *Server) forwardPrometheusRequest(w http.ResponseWriter, r *http.Request, targetPath string) {
	cfg := s.tel.GetConfig()
	promBaseURL := strings.TrimRight(cfg.PrometheusURL, "/")
	if promBaseURL == "" {
		promBaseURL = "http://localhost:9090"
	}

	targetURL, err := url.Parse(promBaseURL + targetPath)
	if err != nil {
		utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Invalid Prometheus target configuration"})
		return
	}

	// Preserve incoming query string parameters
	targetURL.RawQuery = r.URL.RawQuery

	ctx, cancel := context.WithTimeout(r.Context(), proxyTimeoutSec*time.Second)
	defer cancel()

	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL.String(), nil)
	if err != nil {
		utils.WriteJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create backend request"})
		return
	}

	// Server-side authentication injection if configured
	if cfg.PrometheusBearerToken != "" {
		proxyReq.Header.Set("Authorization", "Bearer "+cfg.PrometheusBearerToken)
	} else if cfg.PrometheusBasicAuthUser != "" {
		proxyReq.SetBasicAuth(cfg.PrometheusBasicAuthUser, cfg.PrometheusBasicAuthPass)
	}

	proxyReq.Header.Set("Accept", "application/json")

	resp, err := proxyHttpClient.Do(proxyReq)
	if err != nil {
		s.tel.LogErrorln(r.Context(), "Prometheus proxy query failed", "error", err.Error())
		utils.WriteJSON(w, http.StatusBadGateway, map[string]string{"error": "Failed to connect to Prometheus service"})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)

	// Stream response body capped at maxResponseBodyBytes (2MB)
	limitedReader := io.LimitReader(resp.Body, maxResponseBodyBytes)
	_, _ = io.Copy(w, limitedReader)
}

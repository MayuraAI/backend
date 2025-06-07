package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"gateway/pkg/logger"
	"gateway/services"
)

type HealthResponse struct {
	Status       string                   `json:"status"`
	Timestamp    time.Time                `json:"timestamp"`
	Service      string                   `json:"service"`
	Version      string                   `json:"version"`
	Dependencies map[string]ServiceHealth `json:"dependencies,omitempty"`
}

type ServiceHealth struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

// HealthHandler provides a comprehensive health check endpoint
func HealthHandler(w http.ResponseWriter, r *http.Request) {
	log := logger.GetLogger("health")

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	log.Info("Health check requested")

	// Check dependency health
	dependencies := make(map[string]ServiceHealth)
	overallStatus := "healthy"

	// Check Ollama service
	if err := services.GetOllamaHealth(); err != nil {
		dependencies["ollama"] = ServiceHealth{
			Status:  "unhealthy",
			Message: err.Error(),
		}
		overallStatus = "degraded"
	} else {
		dependencies["ollama"] = ServiceHealth{
			Status: "healthy",
		}
	}

	// Check Gemini service
	if err := services.GetGeminiHealth(); err != nil {
		dependencies["gemini"] = ServiceHealth{
			Status:  "unhealthy",
			Message: err.Error(),
		}
		overallStatus = "degraded"
	} else {
		dependencies["gemini"] = ServiceHealth{
			Status: "healthy",
		}
	}

	// Check classifier service
	if _, err := services.CallModelService("health check"); err != nil {
		dependencies["classifier"] = ServiceHealth{
			Status:  "unhealthy",
			Message: err.Error(),
		}
		if overallStatus == "healthy" {
			overallStatus = "degraded"
		}
	} else {
		dependencies["classifier"] = ServiceHealth{
			Status: "healthy",
		}
	}

	response := HealthResponse{
		Status:       overallStatus,
		Timestamp:    time.Now(),
		Service:      "gateway",
		Version:      "1.0.0",
		Dependencies: dependencies,
	}

	w.Header().Set("Content-Type", "application/json")

	// Set appropriate status code based on health
	switch overallStatus {
	case "healthy":
		w.WriteHeader(http.StatusOK)
	case "degraded":
		w.WriteHeader(http.StatusOK) // Still operational but with issues
	default:
		w.WriteHeader(http.StatusServiceUnavailable)
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
}

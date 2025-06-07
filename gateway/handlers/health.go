package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"gateway/pkg/logger"
)

type HealthResponse struct {
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
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

	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Service:   "gateway",
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "Error encoding response", http.StatusInternalServerError)
		return
	}
}

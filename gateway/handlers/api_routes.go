package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
)

// API version constant - can be changed for future versions
const APIVersion = "v1"

// SetupAPIRoutes sets up all the API routes using separate route modules
func SetupAPIRoutes(mux *http.ServeMux) {
	// Setup routes from separate modules
	SetupProfileRoutes(mux, APIVersion)
	SetupChatRoutes(mux, APIVersion)
	SetupMessageRoutes(mux, APIVersion)
}

// Helper function to extract path parameter
func extractPathParam(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	param := strings.TrimPrefix(path, prefix)
	return strings.TrimSuffix(param, "/")
}

// Helper function to send JSON response
func sendJSONResponse(w http.ResponseWriter, data interface{}, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}

// Helper function to send error response
func sendAPIErrorResponse(w http.ResponseWriter, message string, statusCode int) {
	sendJSONResponse(w, map[string]string{"error": message}, statusCode)
}

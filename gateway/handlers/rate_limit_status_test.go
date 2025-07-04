package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRateLimitStatusHandler_Authenticated(t *testing.T) {
	// Skip this test for now as it requires complex Firebase UserRecord mocking
	t.Skip("Skipping Firebase UserRecord test - requires proper Firebase initialization")
}

func TestRateLimitStatusHandler_Unauthenticated(t *testing.T) {
	// Create a request without authentication
	req := httptest.NewRequest("GET", "/rate-limit-status", nil)
	rr := httptest.NewRecorder()

	// Call the handler
	RateLimitStatusHandler(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Parse response
	var status RateLimitStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Errorf("Error parsing JSON response: %v", err)
	}

	// Verify response structure for unauthenticated user
	if status.DailyLimit <= 0 {
		t.Error("Expected DailyLimit to be greater than 0")
	}
	if status.UserID != "" {
		t.Error("Expected UserID to be empty for unauthenticated user")
	}
	if status.UserEmail != "" {
		t.Error("Expected UserEmail to be empty for unauthenticated user")
	}
}

func TestRateLimitStatusHandler_Options(t *testing.T) {
	// Test OPTIONS request (CORS preflight)
	req := httptest.NewRequest("OPTIONS", "/rate-limit-status", nil)
	rr := httptest.NewRecorder()

	RateLimitStatusHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200 for OPTIONS, got %d", rr.Code)
	}

	// Check CORS headers
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Expected Access-Control-Allow-Origin *, got %s", origin)
	}
}

func TestRateLimitStatusHandler_InvalidMethod(t *testing.T) {
	// Test POST request (should fail)
	req := httptest.NewRequest("POST", "/rate-limit-status", nil)
	rr := httptest.NewRecorder()

	RateLimitStatusHandler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405 for POST, got %d", rr.Code)
	}
}

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"gateway/middleware"

	"github.com/google/uuid"
	"github.com/supabase-community/auth-go/types"
)

func TestRateLimitStatusHandler_Authenticated(t *testing.T) {
	// Create a mock user
	userID := uuid.New()
	mockUser := &types.User{
		ID:    userID,
		Email: "test@example.com",
	}

	// Create a request
	req := httptest.NewRequest("GET", "/rate-limit-status", nil)

	// Add user to context
	ctx := context.WithValue(req.Context(), middleware.SupabaseUserContextKey, mockUser)
	req = req.WithContext(ctx)

	// Create response recorder
	rr := httptest.NewRecorder()

	// Call the handler
	RateLimitStatusHandler(rr, req)

	// Check response
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	// Check content type
	if contentType := rr.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", contentType)
	}

	// Parse response
	var status RateLimitStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &status); err != nil {
		t.Errorf("Error parsing JSON response: %v", err)
	}

	// Verify response structure
	if status.DailyLimit <= 0 {
		t.Error("Expected DailyLimit to be greater than 0")
	}
	if status.RequestsRemaining < 0 {
		t.Error("Expected RequestsRemaining to be >= 0")
	}
	if status.UserID != userID.String() {
		t.Errorf("Expected UserID %s, got %s", userID.String(), status.UserID)
	}
	if status.UserEmail != "test@example.com" {
		t.Errorf("Expected UserEmail test@example.com, got %s", status.UserEmail)
	}
	if status.CurrentMode != middleware.ProRequest {
		t.Errorf("Expected CurrentMode to be pro for new user, got %s", string(status.CurrentMode))
	}
	if status.Message == "" {
		t.Error("Expected Message to be set")
	}
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

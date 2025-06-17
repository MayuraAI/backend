package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

type mockUser struct {
	ID string
}

// func GetSupabaseUserFromContext(ctx any) (*mockUser, bool) {
// 	return nil, false // simulate unauthenticated user for IP-based limiting
// }

func TestRateLimitMiddleware_ProRequestsWithinLimit(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerDay:  5,
		CleanupInterval: 24 * time.Hour,
		CleanupTTL:      48 * time.Hour,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl // override for test

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check request type from context
		requestType, ok := GetRequestTypeFromContext(r.Context())
		if !ok {
			t.Error("Expected request type in context")
			return
		}

		if requestType != ProRequest {
			t.Errorf("Expected ProRequest, got %s", string(requestType))
			return
		}

		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// First 5 requests should be pro requests
	for i := 0; i < 5; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}

		// Check headers
		limit := rr.Header().Get("X-RateLimit-Limit")
		if limit != "5" {
			t.Errorf("Expected X-RateLimit-Limit=5, got %s", limit)
		}

		remaining, _ := strconv.Atoi(rr.Header().Get("X-RateLimit-Remaining"))
		expectedRemaining := 5 - (i + 1)
		if remaining != expectedRemaining {
			t.Errorf("Expected X-RateLimit-Remaining=%d, got %d", expectedRemaining, remaining)
		}

		requestType := rr.Header().Get("X-Request-Type")
		if requestType != "pro" {
			t.Errorf("Expected X-Request-Type=pro, got %s", requestType)
		}
	}
}

func TestRateLimitMiddleware_FreeRequestsAfterLimit(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerDay:  3,
		CleanupInterval: 24 * time.Hour,
		CleanupTTL:      48 * time.Hour,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// First 3 requests should be pro
	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}

		requestType := rr.Header().Get("X-Request-Type")
		if requestType != "pro" {
			t.Errorf("Request %d: Expected X-Request-Type=pro, got %s", i+1, requestType)
		}
	}

	// Next requests should be free
	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}

		requestType := rr.Header().Get("X-Request-Type")
		if requestType != "free" {
			t.Errorf("Free request %d: Expected X-Request-Type=free, got %s", i+1, requestType)
		}

		remaining, _ := strconv.Atoi(rr.Header().Get("X-RateLimit-Remaining"))
		if remaining != 0 {
			t.Errorf("Expected X-RateLimit-Remaining=0 for free requests, got %d", remaining)
		}
	}
}

func TestRateLimitMiddleware_DailyReset(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerDay:  2,
		CleanupInterval: 24 * time.Hour,
		CleanupTTL:      48 * time.Hour,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// Use the first request to get a usage tracker
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Manually reset the usage to simulate next day
	key := "ip:127.0.0.1"
	usage := globalRateLimiter.GetOrCreateUsage(key)

	// Set the reset time to past (simulate new day)
	usage.mutex.Lock()
	usage.ResetTime = time.Now().Add(-1 * time.Hour)
	usage.mutex.Unlock()

	// Next request should be pro again (after reset)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 after reset, got %d", rr.Code)
	}

	requestType := rr.Header().Get("X-Request-Type")
	if requestType != "pro" {
		t.Errorf("Expected pro request after reset, got %s", requestType)
	}
}

func TestRateLimitMiddleware_HeadersSet(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerDay:  10,
		CleanupInterval: 24 * time.Hour,
		CleanupTTL:      48 * time.Hour,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	limit := rr.Header().Get("X-RateLimit-Limit")
	remaining := rr.Header().Get("X-RateLimit-Remaining")
	reset := rr.Header().Get("X-RateLimit-Reset")
	requestType := rr.Header().Get("X-Request-Type")

	if limit != strconv.Itoa(cfg.RequestsPerDay) {
		t.Errorf("Expected X-RateLimit-Limit=%d, got %s", cfg.RequestsPerDay, limit)
	}
	if remaining == "" {
		t.Error("Expected X-RateLimit-Remaining to be set")
	}
	if reset == "" {
		t.Error("Expected X-RateLimit-Reset to be set")
	}
	if requestType == "" {
		t.Error("Expected X-Request-Type to be set")
	}
}

func TestGetRequestTypeFromContext(t *testing.T) {
	// Test pro request
	ctx := context.WithValue(context.Background(), RequestTypeContextKey, ProRequest)
	requestType, ok := GetRequestTypeFromContext(ctx)

	if !ok {
		t.Error("Expected to find request type in context")
	}
	if requestType != ProRequest {
		t.Errorf("Expected ProRequest, got %s", string(requestType))
	}

	// Test free request
	ctx = context.WithValue(context.Background(), RequestTypeContextKey, FreeRequest)
	requestType, ok = GetRequestTypeFromContext(ctx)

	if !ok {
		t.Error("Expected to find request type in context")
	}
	if requestType != FreeRequest {
		t.Errorf("Expected FreeRequest, got %s", string(requestType))
	}

	// Test missing context
	_, ok = GetRequestTypeFromContext(context.Background())
	if ok {
		t.Error("Expected not to find request type in empty context")
	}
}

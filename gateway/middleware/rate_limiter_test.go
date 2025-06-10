package middleware

import (
	"encoding/json"
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

func TestRateLimitMiddleware_AllowsWithinLimit(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 6,
		BurstSize:         3,
		CleanupInterval:   1 * time.Minute,
		CleanupTTL:        1 * time.Minute,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl // override for test

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
	}
}

func TestRateLimitMiddleware_BlocksAfterBurst(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         2,
		CleanupInterval:   1 * time.Minute,
		CleanupTTL:        1 * time.Minute,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	for i := 0; i < 2; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("Expected 200, got %d", rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_RefillsTokens(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 60, // 1 per second
		BurstSize:         1,
		CleanupInterval:   1 * time.Minute,
		CleanupTTL:        1 * time.Minute,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// First request should pass
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d", rr.Code)
	}

	// Second request immediately should fail
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected 429, got %d", rr.Code)
	}

	// Wait for refill
	time.Sleep(1100 * time.Millisecond) // Wait more than 1 token/sec

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("Expected 200 after refill, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_HeadersSet(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 2,
		BurstSize:         1,
		CleanupInterval:   1 * time.Minute,
		CleanupTTL:        1 * time.Minute,
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

	if limit != strconv.Itoa(cfg.RequestsPerMinute) {
		t.Errorf("Expected X-RateLimit-Limit=%d, got %s", cfg.RequestsPerMinute, limit)
	}
	if remaining == "" {
		t.Error("Expected X-RateLimit-Remaining to be set")
	}
	if reset == "" {
		t.Error("Expected X-RateLimit-Reset to be set")
	}
}

func TestRateLimitMiddleware_ErrorResponseJSON(t *testing.T) {
	cfg := RateLimitConfig{
		RequestsPerMinute: 1,
		BurstSize:         1,
		CleanupInterval:   1 * time.Minute,
		CleanupTTL:        1 * time.Minute,
	}

	rl := NewRateLimiter(cfg)
	globalRateLimiter = rl

	handler := RateLimitMiddleware(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"

	// First request OK
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Second request fails
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Errorf("Expected JSON body, got: %s", rr.Body.String())
	}
	if resp["status"] != float64(429) {
		t.Errorf("Expected status 429 in body, got %v", resp["status"])
	}
	if _, ok := resp["error"]; !ok {
		t.Error("Expected 'error' field in JSON response")
	}
}

package middleware

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DailyUsage represents daily usage tracking for a user/IP
type DailyUsage struct {
	RequestCount int       // Number of requests made today
	ResetTime    time.Time // When the daily limit resets (midnight)
	mutex        sync.RWMutex
}

// RateLimiter manages daily usage tracking
type RateLimiter struct {
	usage      map[string]*DailyUsage
	mutex      sync.RWMutex
	cleanupTTL time.Duration
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RequestsPerDay  int           // Daily request limit
	CleanupInterval time.Duration // How often to clean up old usage records
	CleanupTTL      time.Duration // How long to keep inactive usage records
}

// Global instances
var (
	globalRateLimiter *RateLimiter
	defaultConfig     = RateLimitConfig{
		RequestsPerDay:  3,              // 10 requests per day per user
		CleanupInterval: 1 * time.Hour,  // Clean up every hour
		CleanupTTL:      24 * time.Hour, // Keep records for 24 hours
	}
)

// Context keys for request type
type contextKey string

const (
	RequestTypeContextKey contextKey = "request_type"
)

// RequestType represents whether a request is pro or free
type RequestType string

const (
	ProRequest  RequestType = "pro"
	FreeRequest RequestType = "free"
)

// init initializes the global rate limiter
func init() {
	globalRateLimiter = NewRateLimiter(defaultConfig)
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		usage:      make(map[string]*DailyUsage),
		cleanupTTL: config.CleanupTTL,
	}

	// Start cleanup routine
	go rl.cleanupRoutine(config.CleanupInterval)

	return rl
}

// NewDailyUsage creates a new daily usage tracker
func NewDailyUsage() *DailyUsage {
	now := time.Now()
	// Set reset time to next midnight
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())

	return &DailyUsage{
		RequestCount: 0,
		ResetTime:    nextMidnight,
	}
}

// CheckAndIncrementUsage checks if a request should be considered pro or free and increments usage
func (du *DailyUsage) CheckAndIncrementUsage(dailyLimit int) RequestType {
	du.mutex.Lock()
	defer du.mutex.Unlock()

	now := time.Now()

	// Check if we need to reset (new day)
	if now.After(du.ResetTime) {
		du.RequestCount = 0
		// Set reset time to next midnight
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		du.ResetTime = nextMidnight
	}

	// Increment request count
	du.RequestCount++

	// Determine if this is a pro or free request
	if du.RequestCount <= dailyLimit {
		return ProRequest
	}
	return FreeRequest
}

// GetUsageInfo returns current usage information
func (du *DailyUsage) GetUsageInfo() (int, time.Time) {
	du.mutex.RLock()
	defer du.mutex.RUnlock()

	now := time.Now()

	// Check if we need to reset (new day)
	if now.After(du.ResetTime) {
		return 0, time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	}

	return du.RequestCount, du.ResetTime
}

// GetOrCreateUsage gets or creates a daily usage tracker for the given key
func (rl *RateLimiter) GetOrCreateUsage(key string) *DailyUsage {
	rl.mutex.RLock()
	usage, exists := rl.usage[key]
	rl.mutex.RUnlock()

	if exists {
		return usage
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	if usage, exists := rl.usage[key]; exists {
		return usage
	}

	usage = NewDailyUsage()
	rl.usage[key] = usage
	return usage
}

// cleanupRoutine periodically removes old usage records to prevent memory leaks
func (rl *RateLimiter) cleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes old inactive usage records
func (rl *RateLimiter) cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	for key, usage := range rl.usage {
		usage.mutex.RLock()
		// Remove records that haven't been used for more than the TTL
		inactive := now.Sub(usage.ResetTime) > rl.cleanupTTL
		usage.mutex.RUnlock()

		if inactive {
			delete(rl.usage, key)
		}
	}
}

// GetUsageStats returns current usage statistics
func (rl *RateLimiter) GetUsageStats() map[string]interface{} {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	return map[string]interface{}{
		"active_users": len(rl.usage),
		"cleanup_ttl":  rl.cleanupTTL.String(),
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(next http.Handler, config RateLimitConfig) http.Handler {
	// Use provided config or default
	cfg := config

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create rate limit key based on user ID (from auth) or IP address
		key := getRateLimitKey(r)

		// Get or create usage tracker for this key
		usage := globalRateLimiter.GetOrCreateUsage(key)

		// Check and increment usage, get request type
		requestType := usage.CheckAndIncrementUsage(cfg.RequestsPerDay)

		// Get current usage info for headers
		currentCount, resetTime := usage.GetUsageInfo()
		remaining := cfg.RequestsPerDay - currentCount
		if remaining < 0 {
			remaining = 0
		}

		// Log the request with basic info
		log.Printf("[%s] %s %s - %s request (%d/%d used)",
			key, r.Method, r.URL.Path, string(requestType), currentCount, cfg.RequestsPerDay)

		// Add comprehensive rate limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.RequestsPerDay))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		w.Header().Set("X-Request-Type", string(requestType))
		w.Header().Set("X-RateLimit-Used", strconv.Itoa(currentCount))

		// Add user-friendly status message
		var statusMessage string
		if requestType == ProRequest {
			if remaining == 1 {
				statusMessage = "1 pro request remaining today"
			} else {
				statusMessage = fmt.Sprintf("%d pro requests remaining today", remaining)
			}
		} else {
			statusMessage = "All pro requests used - in free mode"
		}
		w.Header().Set("X-RateLimit-Status", statusMessage)

		// Add request type to context for the handler to use
		ctx := context.WithValue(r.Context(), RequestTypeContextKey, requestType)

		// Process the request
		next.ServeHTTP(w, r.WithContext(ctx))

	})
}

// GetRequestTypeFromContext retrieves the request type from the context
func GetRequestTypeFromContext(ctx context.Context) (RequestType, bool) {
	requestType, ok := ctx.Value(RequestTypeContextKey).(RequestType)
	return requestType, ok
}

// getRateLimitKey generates a key for rate limiting based on user ID or IP
func getRateLimitKey(r *http.Request) string {
	// Try to get user ID from context (set by auth middleware)
	if user, ok := GetSupabaseUserFromContext(r.Context()); ok && user != nil {
		return "user:" + user.ID.String()
	}
	return "user:global"
}

// GetRateLimitStats returns current rate limiter statistics
func GetRateLimitStats() map[string]interface{} {
	return globalRateLimiter.GetUsageStats()
}

// GetGlobalRateLimiter returns the global rate limiter instance for direct access
func GetGlobalRateLimiter() *RateLimiter {
	return globalRateLimiter
}

// GetDefaultConfig returns the default rate limiting configuration
func GetDefaultConfig() RateLimitConfig {
	return defaultConfig
}

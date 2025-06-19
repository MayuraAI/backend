package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gateway/pkg/logger"
)

// DailyUsage represents daily usage tracking for a user/IP
type DailyUsage struct {
	RequestCount int       // Number of requests made today
	ResetTime    time.Time // When the daily limit resets (midnight)

	// Suspicious activity tracking
	RequestTimestamps []time.Time // Recent request timestamps for burst detection
	BlockedUntil      time.Time   // When the user/IP is blocked until
	IsBlocked         bool        // Whether the user/IP is currently blocked

	mutex sync.RWMutex
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

	// Suspicious activity detection
	SuspiciousThreshold int           // Max requests allowed in time window
	SuspiciousWindow    time.Duration // Time window for suspicious activity detection
	BlockDuration       time.Duration // How long to block suspicious users/IPs
	TrackingWindow      time.Duration // How long to keep request timestamps
}

// Default rate limiting configuration
var defaultConfig = RateLimitConfig{
	RequestsPerDay:  10,             // 10 requests per day per user
	CleanupInterval: 24 * time.Hour, // Clean up every 24 hours
	CleanupTTL:      48 * time.Hour, // Remove usage records older than 48 hours

	// Suspicious activity defaults
	SuspiciousThreshold: 20,               // 20 requests in 1 minute is suspicious
	SuspiciousWindow:    1 * time.Minute,  // 1 minute window
	BlockDuration:       15 * time.Minute, // Block for 15 minutes
	TrackingWindow:      5 * time.Minute,  // Keep timestamps for 5 minutes
}

// Global rate limiter instance
var globalRateLimiter *RateLimiter

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

// Initialize the rate limiter
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
		RequestCount:      0,
		ResetTime:         nextMidnight,
		RequestTimestamps: make([]time.Time, 0),
		BlockedUntil:      time.Time{},
		IsBlocked:         false,
	}
}

// CheckAndIncrementUsage checks if a request should be considered pro or free and increments usage
func (du *DailyUsage) CheckAndIncrementUsage(dailyLimit int, config RateLimitConfig) (RequestType, bool) {
	du.mutex.Lock()
	defer du.mutex.Unlock()

	now := time.Now()

	// Check if user/IP is currently blocked
	if du.IsBlocked && now.Before(du.BlockedUntil) {
		return FreeRequest, false // Request is blocked
	}

	// If block period has expired, reset blocking
	if du.IsBlocked && now.After(du.BlockedUntil) {
		du.IsBlocked = false
		du.BlockedUntil = time.Time{}
	}

	// Check if we need to reset (new day)
	if now.After(du.ResetTime) {
		du.RequestCount = 0
		// Set reset time to next midnight
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		du.ResetTime = nextMidnight
		// Clear old timestamps on daily reset
		du.RequestTimestamps = make([]time.Time, 0)
	}

	// Add current request timestamp
	du.RequestTimestamps = append(du.RequestTimestamps, now)

	// Clean up old timestamps (keep only those within tracking window)
	cutoff := now.Add(-config.TrackingWindow)
	filteredTimestamps := make([]time.Time, 0)
	for _, ts := range du.RequestTimestamps {
		if ts.After(cutoff) {
			filteredTimestamps = append(filteredTimestamps, ts)
		}
	}
	du.RequestTimestamps = filteredTimestamps

	// Check for suspicious activity (too many requests in short window)
	if du.checkSuspiciousActivity(now, config) {
		du.IsBlocked = true
		du.BlockedUntil = now.Add(config.BlockDuration)
		return FreeRequest, false // Request is blocked due to suspicious activity
	}

	// Increment request count
	du.RequestCount++

	// Determine if this is a pro or free request
	if du.RequestCount <= dailyLimit {
		return ProRequest, true
	}
	return FreeRequest, true
}

// checkSuspiciousActivity checks if the current request pattern is suspicious
func (du *DailyUsage) checkSuspiciousActivity(now time.Time, config RateLimitConfig) bool {
	if config.SuspiciousThreshold <= 0 {
		return false // Suspicious activity detection disabled
	}

	// Count requests within the suspicious window
	cutoff := now.Add(-config.SuspiciousWindow)
	count := 0
	for _, ts := range du.RequestTimestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count > config.SuspiciousThreshold
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

// GetBlockingInfo returns current blocking status information
func (du *DailyUsage) GetBlockingInfo() (bool, time.Time, int) {
	du.mutex.RLock()
	defer du.mutex.RUnlock()

	now := time.Now()

	// Check if user is currently blocked
	isBlocked := du.IsBlocked && now.Before(du.BlockedUntil)

	// Count recent requests for burst tracking
	cutoff := now.Add(-1 * time.Minute) // Last minute
	recentRequests := 0
	for _, ts := range du.RequestTimestamps {
		if ts.After(cutoff) {
			recentRequests++
		}
	}

	return isBlocked, du.BlockedUntil, recentRequests
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

	// Count blocked users
	blockedUsers := 0
	totalRecentRequests := 0
	now := time.Now()

	for _, usage := range rl.usage {
		usage.mutex.RLock()
		if usage.IsBlocked && now.Before(usage.BlockedUntil) {
			blockedUsers++
		}

		// Count recent requests (last minute)
		cutoff := now.Add(-1 * time.Minute)
		for _, ts := range usage.RequestTimestamps {
			if ts.After(cutoff) {
				totalRecentRequests++
			}
		}
		usage.mutex.RUnlock()
	}

	return map[string]interface{}{
		"active_users":         len(rl.usage),
		"blocked_users":        blockedUsers,
		"recent_requests":      totalRecentRequests,
		"cleanup_ttl":          rl.cleanupTTL.String(),
		"suspicious_threshold": defaultConfig.SuspiciousThreshold,
		"suspicious_window":    defaultConfig.SuspiciousWindow.String(),
		"block_duration":       defaultConfig.BlockDuration.String(),
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(next http.Handler, config RateLimitConfig) http.Handler {
	// Use provided config or default
	cfg := config

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := logger.GetLogger("rate_limiter")

		// Create rate limit key based on user ID (from auth) or IP address
		key := getRateLimitKey(r)

		// Get or create usage tracker for this key
		usage := globalRateLimiter.GetOrCreateUsage(key)

		// Check and increment usage, get request type
		requestType, allowed := usage.CheckAndIncrementUsage(cfg.RequestsPerDay, cfg)

		// If request is blocked due to suspicious activity, return 429
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Blocked", "true")
			w.Header().Set("X-RateLimit-Block-Reason", "suspicious-activity")
			w.WriteHeader(http.StatusTooManyRequests)

			// Get block info
			usage.mutex.RLock()
			blockedUntil := usage.BlockedUntil
			usage.mutex.RUnlock()

			response := map[string]interface{}{
				"error":         "Too many requests - suspicious activity detected",
				"code":          "SUSPICIOUS_ACTIVITY_BLOCKED",
				"blocked_until": blockedUntil.Format(time.RFC3339),
				"retry_after":   int(time.Until(blockedUntil).Seconds()),
			}
			json.NewEncoder(w).Encode(response)

			// Log the blocked request
			log.WarnWithFields("Request blocked due to suspicious activity", map[string]interface{}{
				"key":           key,
				"path":          r.URL.Path,
				"blocked_until": blockedUntil.Format(time.RFC3339),
				"user_agent":    r.Header.Get("User-Agent"),
				"ip":            r.RemoteAddr,
			})
			return
		}

		// Get current usage info for headers
		currentCount, resetTime := usage.GetUsageInfo()
		remaining := cfg.RequestsPerDay - currentCount
		if remaining < 0 {
			remaining = 0
		}

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

		// Log the request with comprehensive information
		log.InfoWithFields("Request processed", map[string]interface{}{
			"key":          key,
			"request_type": string(requestType),
			"count":        currentCount,
			"remaining":    remaining,
			"daily_limit":  cfg.RequestsPerDay,
			"reset_time":   resetTime.Format(time.RFC3339),
			"path":         r.URL.Path,
			"status":       statusMessage,
		})

		// Add request type to context for the handler to use
		ctx := context.WithValue(r.Context(), RequestTypeContextKey, requestType)

		// Continue to next handler
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

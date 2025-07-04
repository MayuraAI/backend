package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"gateway/pkg/logger"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// DailyUsage represents daily usage tracking for a user/IP
type DailyUsage struct {
	RequestCount int       // Number of requests made today
	ResetTime    time.Time // When the daily limit resets (midnight)

	// Per-minute rate limiting
	MinuteRequestCount int       // Number of requests made in current minute
	MinuteResetTime    time.Time // When the minute limit resets

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
	RequestsPerDay    int           // Daily request limit
	RequestsPerMinute int           // Per-minute request limit
	CleanupInterval   time.Duration // How often to clean up old usage records
	CleanupTTL        time.Duration // How long to keep inactive usage records

	// Suspicious activity detection
	SuspiciousThreshold int           // Max requests allowed in time window
	SuspiciousWindow    time.Duration // Time window for suspicious activity detection
	BlockDuration       time.Duration // How long to block suspicious users/IPs
	TrackingWindow      time.Duration // How long to keep request timestamps
}

// Default rate limiting configuration
var defaultConfig = RateLimitConfig{
	RequestsPerDay:    10,             // 10 requests per day per authenticated user
	RequestsPerMinute: 3,              // 3 requests per minute per user
	CleanupInterval:   24 * time.Hour, // Clean up every 24 hours
	CleanupTTL:        48 * time.Hour, // Remove usage records older than 48 hours

	// Suspicious activity defaults
	SuspiciousThreshold: 15,               // 15 requests in 5 minutes is suspicious
	SuspiciousWindow:    5 * time.Minute,  // 5 minute window
	BlockDuration:       60 * time.Minute, // Block for 1 hour
	TrackingWindow:      10 * time.Minute, // Keep timestamps for 10 minutes
}

// Anonymous user rate limiting configuration
var anonymousConfig = RateLimitConfig{
	RequestsPerDay:    2,              // 5 requests per day for anonymous users
	RequestsPerMinute: 2,              // 2 requests per minute for anonymous users
	CleanupInterval:   24 * time.Hour, // Clean up every 24 hours
	CleanupTTL:        48 * time.Hour, // Remove usage records older than 48 hours

	// Suspicious activity defaults
	SuspiciousThreshold: 10,               // 10 requests in 5 minutes is suspicious for anonymous
	SuspiciousWindow:    5 * time.Minute,  // 5 minute window
	BlockDuration:       60 * time.Minute, // Block for 1 hour
	TrackingWindow:      10 * time.Minute, // Keep timestamps for 10 minutes
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
	// Set minute reset time to next minute
	nextMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())

	return &DailyUsage{
		RequestCount:       0,
		ResetTime:          nextMidnight,
		MinuteRequestCount: 0,
		MinuteResetTime:    nextMinute,
		RequestTimestamps:  make([]time.Time, 0),
		BlockedUntil:       time.Time{},
		IsBlocked:          false,
	}
}

// CheckAndIncrementUsage checks if a request should be considered pro or free and increments usage
func (du *DailyUsage) CheckAndIncrementUsage(dailyLimit int, config RateLimitConfig, isAnonymous bool) (RequestType, bool) {
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

	// Check if we need to reset daily counter (new day)
	if now.After(du.ResetTime) {
		du.RequestCount = 0
		// Set reset time to next midnight
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		du.ResetTime = nextMidnight
		// Clear old timestamps on daily reset
		du.RequestTimestamps = make([]time.Time, 0)
	}

	// Check if we need to reset minute counter (new minute)
	if now.After(du.MinuteResetTime) {
		du.MinuteRequestCount = 0
		// Set reset time to next minute
		nextMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
		du.MinuteResetTime = nextMinute
	}

	// Check per-minute rate limit first
	if du.MinuteRequestCount >= config.RequestsPerMinute {
		return FreeRequest, false // Request is rate limited by per-minute limit
	}

	// For anonymous users, block entirely if they've exceeded their daily limit
	if isAnonymous && du.RequestCount >= dailyLimit {
		return FreeRequest, false // Block anonymous users after quota exhaustion
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

	// Increment both daily and minute request counts
	du.RequestCount++
	du.MinuteRequestCount++

	// Determine if this is a pro or free request based on daily limit
	if du.RequestCount <= dailyLimit {
		return ProRequest, true
	}
	// For authenticated users, allow free requests after quota exhaustion
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
func (du *DailyUsage) GetUsageInfo() (int, time.Time, int, time.Time) {
	du.mutex.RLock()
	defer du.mutex.RUnlock()

	now := time.Now()

	dailyCount := du.RequestCount
	dailyReset := du.ResetTime
	minuteCount := du.MinuteRequestCount
	minuteReset := du.MinuteResetTime

	// Check if we need to reset daily counter (new day)
	if now.After(du.ResetTime) {
		dailyCount = 0
		dailyReset = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	}

	// Check if we need to reset minute counter (new minute)
	if now.After(du.MinuteResetTime) {
		minuteCount = 0
		minuteReset = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
	}

	return dailyCount, dailyReset, minuteCount, minuteReset
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
		"requests_per_day":     defaultConfig.RequestsPerDay,
		"requests_per_minute":  defaultConfig.RequestsPerMinute,
		"suspicious_threshold": defaultConfig.SuspiciousThreshold,
		"suspicious_window":    defaultConfig.SuspiciousWindow.String(),
		"block_duration":       defaultConfig.BlockDuration.String(),
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(next http.Handler, config RateLimitConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create rate limit key based on user ID (from auth) or IP address
		key := getRateLimitKey(r)

		// Get user from context to determine if anonymous
		user, userOk := GetFirebaseUserFromContext(r.Context())
		var isAnonymous bool
		if userOk && user != nil {
			isAnonymous = IsAnonymousUser(user)
		} else {
			isAnonymous = true // Default to anonymous if no user found
		}

		// Determine which config to use based on user type
		var cfg RateLimitConfig
		if isAnonymous {
			cfg = anonymousConfig
		} else {
			cfg = defaultConfig
		}

		// Get or create usage tracker for this key
		usage := globalRateLimiter.GetOrCreateUsage(key)

		// Check and increment usage, get request type
		requestType, allowed := usage.CheckAndIncrementUsage(cfg.RequestsPerDay, cfg, isAnonymous)

		// If request is blocked, return 429
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Blocked", "true")

			// Get current minute usage to determine block reason
			_, _, minuteCount, minuteReset := usage.GetUsageInfo()

			var blockReason string
			var errorMessage string
			var retryAfter int

			// Determine the reason for blocking
			if minuteCount >= cfg.RequestsPerMinute {
				blockReason = "per-minute-limit"
				errorMessage = fmt.Sprintf("Too many requests - maximum %d requests per minute allowed", cfg.RequestsPerMinute)
				retryAfter = int(time.Until(minuteReset).Seconds())
			} else if isAnonymous {
				// For anonymous users who've exceeded daily quota
				blockReason = "daily-quota-exceeded"
				errorMessage = "You've used all your free requests for today. Sign up to get more!"
				retryAfter = int(time.Until(time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location())).Seconds())
			} else {
				// Must be suspicious activity
				blockReason = "suspicious-activity"
				errorMessage = "Too many requests - suspicious activity detected"

				// Get block info for suspicious activity
				usage.mutex.RLock()
				blockedUntil := usage.BlockedUntil
				usage.mutex.RUnlock()
				retryAfter = int(time.Until(blockedUntil).Seconds())
			}

			w.Header().Set("X-RateLimit-Block-Reason", blockReason)
			w.WriteHeader(http.StatusTooManyRequests)

			response := map[string]interface{}{
				"error":       errorMessage,
				"code":        "RATE_LIMIT_EXCEEDED",
				"retry_after": retryAfter,
			}

			if blockReason == "per-minute-limit" {
				response["reset_time"] = minuteReset.Format(time.RFC3339)
			} else if blockReason == "daily-quota-exceeded" {
				response["reset_time"] = time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location()).Format(time.RFC3339)
			} else {
				usage.mutex.RLock()
				response["blocked_until"] = usage.BlockedUntil.Format(time.RFC3339)
				usage.mutex.RUnlock()
			}

			json.NewEncoder(w).Encode(response)

			// Log the blocked request
			logger.GetDailyLogger().Info("Request blocked", "reason", blockReason, "key", key, "path", r.URL.Path)
			return
		}

		// Get current usage info for headers
		currentCount, resetTime, minuteCount, minuteReset := usage.GetUsageInfo()
		remaining := cfg.RequestsPerDay - currentCount
		if remaining < 0 {
			remaining = 0
		}

		minuteRemaining := cfg.RequestsPerMinute - minuteCount
		if minuteRemaining < 0 {
			minuteRemaining = 0
		}

		// Log the request with basic info
		logger.GetDailyLogger().Info("Request processed", "key", key, "method", r.Method, "path", r.URL.Path, "type", string(requestType), "count", currentCount, "daily_limit", cfg.RequestsPerDay, "minute_count", minuteCount, "minute_limit", cfg.RequestsPerMinute)

		// Add comprehensive rate limit headers
		w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.RequestsPerDay))
		w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
		w.Header().Set("X-RateLimit-Limit-Minute", strconv.Itoa(cfg.RequestsPerMinute))
		w.Header().Set("X-RateLimit-Remaining-Minute", strconv.Itoa(minuteRemaining))
		w.Header().Set("X-RateLimit-Reset-Minute", strconv.FormatInt(minuteReset.Unix(), 10))
		w.Header().Set("X-Request-Type", string(requestType))
		w.Header().Set("X-RateLimit-Used", strconv.Itoa(currentCount))

		// Add user-friendly status message
		var statusMessage string
		if requestType == ProRequest {
			if remaining == 1 {
				statusMessage = fmt.Sprintf("1 pro request remaining today (%d/min remaining)", minuteRemaining)
			} else {
				statusMessage = fmt.Sprintf("%d pro requests remaining today (%d/min remaining)", remaining, minuteRemaining)
			}
		} else {
			statusMessage = fmt.Sprintf("All pro requests used - in free mode (%d/min remaining)", minuteRemaining)
		}
		w.Header().Set("X-RateLimit-Status", statusMessage)

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
	if user, ok := GetFirebaseUserFromContext(r.Context()); ok && user != nil {
		if IsAnonymousUser(user) {
			return "anonymous:" + user.UID
		}
		return "user:" + user.UID
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

// GetAnonymousConfig returns the anonymous user rate limiting configuration
func GetAnonymousConfig() RateLimitConfig {
	return anonymousConfig
}

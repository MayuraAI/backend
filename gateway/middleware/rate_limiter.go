package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"gateway/pkg/logger"
	"gateway/pkg/redis"
	"net/http"
	"strconv"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
)

// DailyUsage represents daily usage tracking for a user/IP stored in Redis
type DailyUsage struct {
	RequestCount int       `json:"request_count"` // Number of requests made today
	ResetTime    time.Time `json:"reset_time"`    // When the daily limit resets (midnight)

	// Per-minute rate limiting
	MinuteRequestCount int       `json:"minute_request_count"` // Number of requests made in current minute
	MinuteResetTime    time.Time `json:"minute_reset_time"`    // When the minute limit resets

	// Suspicious activity tracking
	RequestTimestamps []time.Time `json:"request_timestamps"` // Recent request timestamps for burst detection
	BlockedUntil      time.Time   `json:"blocked_until"`      // When the user/IP is blocked until
	IsBlocked         bool        `json:"is_blocked"`         // Whether the user/IP is currently blocked
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
	RequestsPerDay:    5,             // 5 requests per day per authenticated user
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
	RequestsPerDay:    5,              // 0 requests per day for anonymous users
	RequestsPerMinute: 5,              // 2 requests per minute for anonymous users
	CleanupInterval:   24 * time.Hour, // Clean up every 24 hours
	CleanupTTL:        48 * time.Hour, // Remove usage records older than 48 hours

	// Suspicious activity defaults
	SuspiciousThreshold: 10,               // 10 requests in 5 minutes is suspicious for anonymous
	SuspiciousWindow:    5 * time.Minute,  // 5 minute window
	BlockDuration:       60 * time.Minute, // Block for 1 hour
	TrackingWindow:      10 * time.Minute, // Keep timestamps for 10 minutes
}

// Global rate limiter instance - no longer needed with Redis
// var globalRateLimiter *RateLimiter

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

// Redis key prefixes
const (
	rateLimitPrefix = "rate_limit:"
	usageKeyPrefix  = "usage:"
)

// init initializes the global rate limiter - no longer needed
// func init() {
//	globalRateLimiter = NewRateLimiter(defaultConfig)
// }

// NewRateLimiter creates a new rate limiter with the given configuration - no longer needed with Redis
// func NewRateLimiter(config RateLimitConfig) *RateLimiter {
//	rl := &RateLimiter{
//		usage:      make(map[string]*DailyUsage),
//		cleanupTTL: config.CleanupTTL,
//	}
//
//	// Start cleanup routine
//	go rl.cleanupRoutine(config.CleanupInterval)
//
//	return rl
// }

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

// getUsageFromRedis retrieves usage data from Redis
func getUsageFromRedis(ctx context.Context, key string) (*DailyUsage, error) {
	client := redis.GetClient()
	if client == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	usageKey := rateLimitPrefix + usageKeyPrefix + key

	// Get usage data from Redis
	data, err := client.Get(ctx, usageKey).Result()
	if err != nil {
		if err == redisv9.Nil {
			// Key doesn't exist, return new usage
			return NewDailyUsage(), nil
		}
		return nil, fmt.Errorf("failed to get usage from redis: %w", err)
	}

	// Unmarshal JSON data
	var usage DailyUsage
	if err := json.Unmarshal([]byte(data), &usage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal usage data: %w", err)
	}

	return &usage, nil
}

// saveUsageToRedis saves usage data to Redis with TTL
func saveUsageToRedis(ctx context.Context, key string, usage *DailyUsage, ttl time.Duration) error {
	client := redis.GetClient()
	if client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	usageKey := rateLimitPrefix + usageKeyPrefix + key

	// Marshal usage data to JSON
	data, err := json.Marshal(usage)
	if err != nil {
		return fmt.Errorf("failed to marshal usage data: %w", err)
	}

	// Save to Redis with TTL
	err = client.Set(ctx, usageKey, data, ttl).Err()
	if err != nil {
		return fmt.Errorf("failed to save usage to redis: %w", err)
	}

	return nil
}

// CheckAndIncrementUsage checks if a request should be considered pro or free and increments usage
func CheckAndIncrementUsage(ctx context.Context, key string, dailyLimit int, config RateLimitConfig, isAnonymous bool) (RequestType, bool, error) {
	// Get current usage from Redis
	usage, err := getUsageFromRedis(ctx, key)
	if err != nil {
		return FreeRequest, false, err
	}

	now := time.Now()

	// Check if user/IP is currently blocked
	if usage.IsBlocked && now.Before(usage.BlockedUntil) {
		return FreeRequest, false, nil // Request is blocked
	}

	// If block period has expired, reset blocking
	if usage.IsBlocked && now.After(usage.BlockedUntil) {
		usage.IsBlocked = false
		usage.BlockedUntil = time.Time{}
	}

	// Check if we need to reset daily counter (new day)
	if now.After(usage.ResetTime) {
		usage.RequestCount = 0
		// Set reset time to next midnight
		nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
		usage.ResetTime = nextMidnight
		// Clear old timestamps on daily reset
		usage.RequestTimestamps = make([]time.Time, 0)
	}

	// Check if we need to reset minute counter (new minute)
	if now.After(usage.MinuteResetTime) {
		usage.MinuteRequestCount = 0
		// Set reset time to next minute
		nextMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
		usage.MinuteResetTime = nextMinute
	}

	// Check per-minute rate limit first
	if usage.MinuteRequestCount >= config.RequestsPerMinute {
		// Save current state to Redis
		saveUsageToRedis(ctx, key, usage, config.CleanupTTL)
		return FreeRequest, false, nil // Request is rate limited by per-minute limit
	}

	// For anonymous users, block entirely if they've exceeded their daily limit
	if isAnonymous {
		if usage.RequestCount >= dailyLimit {
			// Save current state to Redis
			saveUsageToRedis(ctx, key, usage, config.CleanupTTL)
			return FreeRequest, false, nil
		}

		// Increment both daily and minute request counts for anonymous users
		usage.RequestCount++
		usage.MinuteRequestCount++

		// Save updated usage to Redis
		err = saveUsageToRedis(ctx, key, usage, config.CleanupTTL)
		if err != nil {
			return FreeRequest, false, err
		}

		return FreeRequest, true, nil
	}

	// Add current request timestamp for authenticated users
	usage.RequestTimestamps = append(usage.RequestTimestamps, now)

	// Clean up old timestamps (keep only those within tracking window)
	cutoff := now.Add(-config.TrackingWindow)
	filteredTimestamps := make([]time.Time, 0)
	for _, ts := range usage.RequestTimestamps {
		if ts.After(cutoff) {
			filteredTimestamps = append(filteredTimestamps, ts)
		}
	}
	usage.RequestTimestamps = filteredTimestamps

	// Check for suspicious activity (too many requests in short window)
	if checkSuspiciousActivity(usage, now, config) {
		usage.IsBlocked = true
		usage.BlockedUntil = now.Add(config.BlockDuration)

		// Save updated usage to Redis
		err = saveUsageToRedis(ctx, key, usage, config.CleanupTTL)
		if err != nil {
			return FreeRequest, false, err
		}

		return FreeRequest, false, nil // Request is blocked due to suspicious activity
	}

	// Increment both daily and minute request counts
	usage.RequestCount++
	usage.MinuteRequestCount++

	// Save updated usage to Redis
	err = saveUsageToRedis(ctx, key, usage, config.CleanupTTL)
	if err != nil {
		return FreeRequest, false, err
	}

	// Determine if this is a pro or free request based on daily limit
	if usage.RequestCount <= dailyLimit {
		return ProRequest, true, nil
	}
	// For authenticated users, allow free requests after quota exhaustion
	return FreeRequest, true, nil
}

// checkSuspiciousActivity checks if the current request pattern is suspicious
func checkSuspiciousActivity(usage *DailyUsage, now time.Time, config RateLimitConfig) bool {
	if config.SuspiciousThreshold <= 0 {
		return false // Suspicious activity detection disabled
	}

	// Count requests within the suspicious window
	cutoff := now.Add(-config.SuspiciousWindow)
	count := 0
	for _, ts := range usage.RequestTimestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count > config.SuspiciousThreshold
}

// GetUsageInfo returns current usage information from Redis
func GetUsageInfo(ctx context.Context, key string) (int, time.Time, int, time.Time, error) {
	usage, err := getUsageFromRedis(ctx, key)
	if err != nil {
		return 0, time.Time{}, 0, time.Time{}, err
	}

	now := time.Now()

	dailyCount := usage.RequestCount
	dailyReset := usage.ResetTime
	minuteCount := usage.MinuteRequestCount
	minuteReset := usage.MinuteResetTime

	// Check if we need to reset daily counter (new day)
	if now.After(usage.ResetTime) {
		dailyCount = 0
		dailyReset = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	}

	// Check if we need to reset minute counter (new minute)
	if now.After(usage.MinuteResetTime) {
		minuteCount = 0
		minuteReset = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
	}

	return dailyCount, dailyReset, minuteCount, minuteReset, nil
}

// GetBlockingInfo returns current blocking status information from Redis
func GetBlockingInfo(ctx context.Context, key string) (bool, time.Time, int, error) {
	usage, err := getUsageFromRedis(ctx, key)
	if err != nil {
		return false, time.Time{}, 0, err
	}

	now := time.Now()

	// Check if user is currently blocked
	isBlocked := usage.IsBlocked && now.Before(usage.BlockedUntil)

	// Count recent requests for burst tracking
	cutoff := now.Add(-1 * time.Minute) // Last minute
	recentRequests := 0
	for _, ts := range usage.RequestTimestamps {
		if ts.After(cutoff) {
			recentRequests++
		}
	}

	return isBlocked, usage.BlockedUntil, recentRequests, nil
}

// GetUsageStats returns current usage statistics from Redis
func GetUsageStats(ctx context.Context) (map[string]interface{}, error) {
	client := redis.GetClient()
	if client == nil {
		return nil, fmt.Errorf("redis client not initialized")
	}

	// Get all usage keys
	pattern := rateLimitPrefix + usageKeyPrefix + "*"
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get usage keys: %w", err)
	}

	// Count blocked users and recent requests
	blockedUsers := 0
	totalRecentRequests := 0
	now := time.Now()

	for _, key := range keys {
		// Remove prefix to get the actual key
		actualKey := key[len(rateLimitPrefix+usageKeyPrefix):]

		usage, err := getUsageFromRedis(ctx, actualKey)
		if err != nil {
			continue // Skip this key if we can't read it
		}

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
	}

	return map[string]interface{}{
		"active_users":         len(keys),
		"blocked_users":        blockedUsers,
		"recent_requests":      totalRecentRequests,
		"cleanup_ttl":          defaultConfig.CleanupTTL.String(),
		"requests_per_day":     defaultConfig.RequestsPerDay,
		"requests_per_minute":  defaultConfig.RequestsPerMinute,
		"suspicious_threshold": defaultConfig.SuspiciousThreshold,
		"suspicious_window":    defaultConfig.SuspiciousWindow.String(),
		"block_duration":       defaultConfig.BlockDuration.String(),
		"storage_backend":      "redis",
	}, nil
}

// CleanupExpiredUsage removes expired usage records from Redis
func CleanupExpiredUsage(ctx context.Context) error {
	client := redis.GetClient()
	if client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	// Get all usage keys
	pattern := rateLimitPrefix + usageKeyPrefix + "*"
	keys, err := client.Keys(ctx, pattern).Result()
	if err != nil {
		return fmt.Errorf("failed to get usage keys: %w", err)
	}

	expiredCount := 0

	for _, key := range keys {
		// Check TTL of the key
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			continue
		}

		// If TTL is -1 (no expiration) or expired, handle accordingly
		if ttl == -1 {
			// Set TTL for keys without expiration
			client.Expire(ctx, key, defaultConfig.CleanupTTL)
		} else if ttl <= 0 {
			// Key is expired, delete it
			client.Del(ctx, key)
			expiredCount++
		}
	}

	logger.GetDailyLogger().Info("Cleaned up expired usage records", "expired_count", expiredCount, "total_keys", len(keys))
	return nil
}

// RateLimitMiddleware creates a rate limiting middleware using Redis
func RateLimitMiddleware(next http.Handler, config RateLimitConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Create rate limit key based on user ID (from auth) or IP address
		key := getRateLimitKey(r)

		// Get user from context to determine if anonymous
		user, userOk := GetFirebaseUserFromContext(ctx)
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

		// Check and increment usage, get request type
		requestType, allowed, err := CheckAndIncrementUsage(ctx, key, cfg.RequestsPerDay, cfg, isAnonymous)
		if err != nil {
			// Log error but don't block request
			logger.GetDailyLogger().Error("Rate limit check failed", "error", err, "key", key)
			// Continue with request as ProRequest if Redis fails
			requestType = ProRequest
			allowed = true
		}

		// If request is blocked, return 429
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Blocked", "true")

			// Get current minute usage to determine block reason
			_, _, minuteCount, minuteReset, err := GetUsageInfo(ctx, key)
			if err != nil {
				// Fallback values if Redis fails
				minuteCount = cfg.RequestsPerMinute
				minuteReset = time.Now().Add(time.Minute)
			}

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
				_, blockedUntil, _, err := GetBlockingInfo(ctx, key)
				if err != nil {
					blockedUntil = time.Now().Add(cfg.BlockDuration)
				}
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
				if _, blockedUntil, _, err := GetBlockingInfo(ctx, key); err == nil {
					response["blocked_until"] = blockedUntil.Format(time.RFC3339)
				}
			}

			json.NewEncoder(w).Encode(response)

			// Log the blocked request
			logger.GetDailyLogger().Info("Request blocked", "reason", blockReason, "key", key, "path", r.URL.Path)
			return
		}

		// Get current usage info for headers
		currentCount, resetTime, minuteCount, minuteReset, err := GetUsageInfo(ctx, key)
		if err != nil {
			// Use fallback values if Redis fails
			currentCount = 0
			resetTime = time.Date(time.Now().Year(), time.Now().Month(), time.Now().Day()+1, 0, 0, 0, 0, time.Now().Location())
			minuteCount = 0
			minuteReset = time.Now().Add(time.Minute)
		}

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
		ctx = context.WithValue(ctx, RequestTypeContextKey, requestType)

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
	ctx := context.Background()
	stats, err := GetUsageStats(ctx)
	if err != nil {
		return map[string]interface{}{
			"error":           err.Error(),
			"storage_backend": "redis",
		}
	}
	return stats
}

// GetDefaultConfig returns the default rate limiting configuration
func GetDefaultConfig() RateLimitConfig {
	return defaultConfig
}

// GetAnonymousConfig returns the anonymous user rate limiting configuration
func GetAnonymousConfig() RateLimitConfig {
	return anonymousConfig
}

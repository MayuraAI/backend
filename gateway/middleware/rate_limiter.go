package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"gateway/config"
	"gateway/pkg/logger"
	"gateway/pkg/redis"
	"net/http"
	"strconv"
	"time"

	redisv9 "github.com/redis/go-redis/v9"
)

// DailyUsage represents daily usage tracking for a user/IP stored in Redis
type DailyUsage struct {
	// Free and Pro request counts
	FreeRequestCount int `json:"free_request_count"` // Number of free requests made
	ProRequestCount  int `json:"pro_request_count"`  // Number of pro requests made

	// Reset times
	ResetTime time.Time `json:"reset_time"` // When the daily limit resets (midnight)

	// Per-minute rate limiting
	MinuteRequestCount int       `json:"minute_request_count"` // Number of requests made in current minute
	MinuteResetTime    time.Time `json:"minute_reset_time"`    // When the minute limit resets

	// Suspicious activity tracking
	RequestTimestamps []time.Time `json:"request_timestamps"` // Recent request timestamps for burst detection
	BlockedUntil      time.Time   `json:"blocked_until"`      // When the user/IP is blocked until
	IsBlocked         bool        `json:"is_blocked"`         // Whether the user/IP is currently blocked

	// Tier information
	UserTier    config.SubscriptionTier `json:"user_tier"`    // User's subscription tier
	IsAnonymous bool                    `json:"is_anonymous"` // Whether user is anonymous
}

// RateLimitConfig holds rate limiting configuration (legacy support)
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

// NewDailyUsage creates a new daily usage tracker
func NewDailyUsage(tier config.SubscriptionTier, isAnonymous bool) *DailyUsage {
	now := time.Now()
	// Set reset time to next midnight (unless it's a lifetime limit)
	nextMidnight := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	// Set minute reset time to next minute
	nextMinute := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())

	return &DailyUsage{
		FreeRequestCount:   0,
		ProRequestCount:    0,
		ResetTime:          nextMidnight,
		MinuteRequestCount: 0,
		MinuteResetTime:    nextMinute,
		RequestTimestamps:  make([]time.Time, 0),
		BlockedUntil:       time.Time{},
		IsBlocked:          false,
		UserTier:           tier,
		IsAnonymous:        isAnonymous,
	}
}

// getUsageFromRedis retrieves usage data from Redis
func getUsageFromRedis(ctx context.Context, key string, tier config.SubscriptionTier, isAnonymous bool) (*DailyUsage, error) {
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
			return NewDailyUsage(tier, isAnonymous), nil
		}
		return nil, fmt.Errorf("failed to get usage from redis: %w", err)
	}

	// Unmarshal JSON data
	var usage DailyUsage
	if err := json.Unmarshal([]byte(data), &usage); err != nil {
		return nil, fmt.Errorf("failed to unmarshal usage data: %w", err)
	}

	// Update tier information in case it changed
	usage.UserTier = tier
	usage.IsAnonymous = isAnonymous

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
func CheckAndIncrementUsage(ctx context.Context, key string, tier config.SubscriptionTier, isAnonymous bool) (RequestType, bool, error) {
	// Get tier configuration
	tierConfig, err := config.GetRateLimitConfig(tier)
	if err != nil {
		return FreeRequest, false, fmt.Errorf("failed to get tier config: %w", err)
	}

	// Get suspicious activity configuration
	suspiciousConfig, err := config.GetSuspiciousActivityConfig()
	if err != nil {
		return FreeRequest, false, fmt.Errorf("failed to get suspicious activity config: %w", err)
	}

	// Get cleanup configuration
	cleanupConfig, err := config.GetCleanupConfig()
	if err != nil {
		return FreeRequest, false, fmt.Errorf("failed to get cleanup config: %w", err)
	}

	// Get current usage from Redis
	usage, err := getUsageFromRedis(ctx, key, tier, isAnonymous)
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

	// Check if we need to reset daily counter (new day) - but not for lifetime limits
	if !tierConfig.LifetimeLimit && tierConfig.DailyReset && now.After(usage.ResetTime) {
		usage.FreeRequestCount = 0
		usage.ProRequestCount = 0
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
	if usage.MinuteRequestCount >= tierConfig.RequestsPerMinute {
		// Save current state to Redis
		saveUsageToRedis(ctx, key, usage, config.GetDurationFromSeconds(cleanupConfig.TTL))
		return FreeRequest, false, nil // Request is rate limited by per-minute limit
	}

	// For anonymous users with lifetime limits, check if they've exceeded their total limit
	if isAnonymous && tierConfig.LifetimeLimit {
		totalRequests := usage.FreeRequestCount + usage.ProRequestCount
		if totalRequests >= tierConfig.RequestsPerDay {
			// Save current state to Redis
			saveUsageToRedis(ctx, key, usage, config.GetDurationFromSeconds(cleanupConfig.TTL))
			return FreeRequest, false, nil // Request is blocked - lifetime limit exceeded
		}
	}

	// For non-anonymous users, check for suspicious activity
	if !isAnonymous {
		// Add current request timestamp
		usage.RequestTimestamps = append(usage.RequestTimestamps, now)

		// Clean up old timestamps (keep only those within tracking window)
		cutoff := now.Add(-config.GetDurationFromSeconds(suspiciousConfig.TrackingWindow))
		filteredTimestamps := make([]time.Time, 0)
		for _, ts := range usage.RequestTimestamps {
			if ts.After(cutoff) {
				filteredTimestamps = append(filteredTimestamps, ts)
			}
		}
		usage.RequestTimestamps = filteredTimestamps

		// Check for suspicious activity (too many requests in short window)
		if checkSuspiciousActivity(usage, now, suspiciousConfig) {
			usage.IsBlocked = true
			usage.BlockedUntil = now.Add(config.GetDurationFromSeconds(suspiciousConfig.BlockDuration))

			// Save updated usage to Redis
			err = saveUsageToRedis(ctx, key, usage, config.GetDurationFromSeconds(cleanupConfig.TTL))
			if err != nil {
				return FreeRequest, false, err
			}

			return FreeRequest, false, nil // Request is blocked due to suspicious activity
		}
	}

	// Determine request type based on tier and current usage
	requestType := determineRequestType(usage, tierConfig)

	// Increment appropriate counters
	if requestType == ProRequest {
		usage.ProRequestCount++
	} else {
		usage.FreeRequestCount++
	}
	usage.MinuteRequestCount++

	// Save updated usage to Redis
	err = saveUsageToRedis(ctx, key, usage, config.GetDurationFromSeconds(cleanupConfig.TTL))
	if err != nil {
		return FreeRequest, false, err
	}

	return requestType, true, nil
}

// determineRequestType determines if a request should be pro or free based on tier and usage
func determineRequestType(usage *DailyUsage, tierConfig config.RateLimitConfig) RequestType {
	// Anonymous users always get free requests
	if usage.IsAnonymous {
		return FreeRequest
	}

	// Check if user has pro requests available
	if tierConfig.ProRequests > 0 && usage.ProRequestCount < tierConfig.ProRequests {
		return ProRequest
	}

	// Check if user has unlimited free requests
	if config.IsUnlimited(tierConfig.FreeRequests) {
		return FreeRequest
	}

	// Check if user has free requests available
	if tierConfig.FreeRequests > 0 && usage.FreeRequestCount < tierConfig.FreeRequests {
		return FreeRequest
	}

	// No requests available - this shouldn't happen if rate limiting is working correctly
	return FreeRequest
}

// checkSuspiciousActivity checks if the current request pattern is suspicious
func checkSuspiciousActivity(usage *DailyUsage, now time.Time, suspiciousConfig config.SuspiciousActivityConfig) bool {
	if suspiciousConfig.Threshold <= 0 {
		return false // Suspicious activity detection disabled
	}

	// Count requests within the suspicious window
	cutoff := now.Add(-config.GetDurationFromSeconds(suspiciousConfig.Window))
	count := 0
	for _, ts := range usage.RequestTimestamps {
		if ts.After(cutoff) {
			count++
		}
	}

	return count > suspiciousConfig.Threshold
}

// GetUsageInfo returns current usage information from Redis
func GetUsageInfo(ctx context.Context, key string, tier config.SubscriptionTier, isAnonymous bool) (int, int, time.Time, int, time.Time, error) {
	usage, err := getUsageFromRedis(ctx, key, tier, isAnonymous)
	if err != nil {
		return 0, 0, time.Time{}, 0, time.Time{}, err
	}

	now := time.Now()
	tierConfig, err := config.GetRateLimitConfig(tier)
	if err != nil {
		return 0, 0, time.Time{}, 0, time.Time{}, err
	}

	freeCount := usage.FreeRequestCount
	proCount := usage.ProRequestCount
	dailyReset := usage.ResetTime
	minuteCount := usage.MinuteRequestCount
	minuteReset := usage.MinuteResetTime

	// Check if we need to reset daily counter (new day) - but not for lifetime limits
	if !tierConfig.LifetimeLimit && tierConfig.DailyReset && now.After(usage.ResetTime) {
		freeCount = 0
		proCount = 0
		dailyReset = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	}

	// Check if we need to reset minute counter (new minute)
	if now.After(usage.MinuteResetTime) {
		minuteCount = 0
		minuteReset = time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), now.Minute()+1, 0, 0, now.Location())
	}

	return freeCount, proCount, dailyReset, minuteCount, minuteReset, nil
}

// GetBlockingInfo returns current blocking status information from Redis
func GetBlockingInfo(ctx context.Context, key string, tier config.SubscriptionTier, isAnonymous bool) (bool, time.Time, int, error) {
	usage, err := getUsageFromRedis(ctx, key, tier, isAnonymous)
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

		// We need tier info to get usage, so we'll use a default tier for stats
		usage, err := getUsageFromRedis(ctx, actualKey, config.TierFree, false)
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
		"active_users":    len(keys),
		"blocked_users":   blockedUsers,
		"recent_requests": totalRecentRequests,
		"storage_backend": "redis",
		"config_source":   "tier_based",
	}, nil
}

// CleanupExpiredUsage removes expired usage records from Redis
func CleanupExpiredUsage(ctx context.Context) error {
	client := redis.GetClient()
	if client == nil {
		return fmt.Errorf("redis client not initialized")
	}

	// Get cleanup configuration
	cleanupConfig, err := config.GetCleanupConfig()
	if err != nil {
		return fmt.Errorf("failed to get cleanup config: %w", err)
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
			client.Expire(ctx, key, config.GetDurationFromSeconds(cleanupConfig.TTL))
		} else if ttl <= 0 {
			// Key is expired, delete it
			client.Del(ctx, key)
			expiredCount++
		}
	}

	logger.GetDailyLogger().Info("Cleaned up expired usage records", "expired_count", expiredCount, "total_keys", len(keys))
	return nil
}

// RateLimitMiddleware creates a rate limiting middleware using Redis and tier-based configuration
func RateLimitMiddleware(next http.Handler, legacyConfig RateLimitConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Create rate limit key based on user ID (from auth) or IP address
		key := getRateLimitKey(r)

		// Get user tier from context (includes subscription service lookup)
		tier, isAnonymous := GetUserTierFromContext(ctx, r)

		// Check and increment usage, get request type
		requestType, allowed, err := CheckAndIncrementUsage(ctx, key, tier, isAnonymous)
		if err != nil {
			// Log error but don't block request
			logger.GetDailyLogger().Error("Rate limit check failed", "error", err, "key", key)
			// Continue with request as FreeRequest if Redis fails
			requestType = FreeRequest
			allowed = true
		}

		// If request is blocked, return 429
		if !allowed {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-RateLimit-Blocked", "true")

			// Get tier config for response headers
			tierConfig, _ := config.GetRateLimitConfig(tier)

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(tierConfig.RequestsPerDay))
			w.Header().Set("X-RateLimit-Remaining", "0")
			w.Header().Set("X-Request-Type", string(requestType))

			w.WriteHeader(http.StatusTooManyRequests)
			json.NewEncoder(w).Encode(map[string]string{
				"error": "Rate limit exceeded. Please try again later.",
				"type":  "rate_limit_exceeded",
			})
			return
		}

		// Add request type to context for downstream handlers
		ctx = context.WithValue(ctx, RequestTypeContextKey, requestType)

		// Get usage info for response headers
		freeCount, proCount, resetTime, _, _, err := GetUsageInfo(ctx, key, tier, isAnonymous)
		if err == nil {
			tierConfig, _ := config.GetRateLimitConfig(tier)

			// Calculate remaining requests based on request type
			var remaining int
			if requestType == ProRequest {
				remaining = max(0, tierConfig.ProRequests-proCount)
			} else {
				if config.IsUnlimited(tierConfig.FreeRequests) {
					remaining = 999999 // Large number to indicate unlimited
				} else {
					remaining = max(0, tierConfig.FreeRequests-freeCount)
				}
			}

			// Set response headers
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(tierConfig.RequestsPerDay))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
			w.Header().Set("X-Request-Type", string(requestType))

			// Set usage headers
			w.Header().Set("X-RateLimit-Used-Free", strconv.Itoa(freeCount))
			w.Header().Set("X-RateLimit-Used-Pro", strconv.Itoa(proCount))
		}

		// Continue with the request
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Helper function to get max of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// GetRequestTypeFromContext extracts the request type from context
func GetRequestTypeFromContext(ctx context.Context) (RequestType, bool) {
	if reqType, ok := ctx.Value(RequestTypeContextKey).(RequestType); ok {
		return reqType, true
	}
	return FreeRequest, false
}

// getRateLimitKey generates a rate limit key based on user ID or IP
func getRateLimitKey(r *http.Request) string {
	// Try to get user ID from context first
	if user, ok := GetFirebaseUserFromContext(r.Context()); ok && user != nil {
		if IsAnonymousUser(user) {
			return "anonymous:" + user.UID
		}
		return "user:" + user.UID
	}

	// Fallback to IP address
	return "ip:" + r.RemoteAddr
}

// Legacy support functions for backward compatibility
func GetRateLimitStats() map[string]interface{} {
	stats, _ := GetUsageStats(context.Background())
	return stats
}

func GetDefaultConfig() RateLimitConfig {
	tierConfig, _ := config.GetRateLimitConfig(config.TierFree)
	suspiciousConfig, _ := config.GetSuspiciousActivityConfig()
	cleanupConfig, _ := config.GetCleanupConfig()

	return RateLimitConfig{
		RequestsPerDay:      tierConfig.RequestsPerDay,
		RequestsPerMinute:   tierConfig.RequestsPerMinute,
		CleanupInterval:     config.GetDurationFromSeconds(cleanupConfig.Interval),
		CleanupTTL:          config.GetDurationFromSeconds(cleanupConfig.TTL),
		SuspiciousThreshold: suspiciousConfig.Threshold,
		SuspiciousWindow:    config.GetDurationFromSeconds(suspiciousConfig.Window),
		BlockDuration:       config.GetDurationFromSeconds(suspiciousConfig.BlockDuration),
		TrackingWindow:      config.GetDurationFromSeconds(suspiciousConfig.TrackingWindow),
	}
}

func GetAnonymousConfig() RateLimitConfig {
	tierConfig, _ := config.GetRateLimitConfig(config.TierAnonymous)
	suspiciousConfig, _ := config.GetSuspiciousActivityConfig()
	cleanupConfig, _ := config.GetCleanupConfig()

	return RateLimitConfig{
		RequestsPerDay:      tierConfig.RequestsPerDay,
		RequestsPerMinute:   tierConfig.RequestsPerMinute,
		CleanupInterval:     config.GetDurationFromSeconds(cleanupConfig.Interval),
		CleanupTTL:          config.GetDurationFromSeconds(cleanupConfig.TTL),
		SuspiciousThreshold: suspiciousConfig.Threshold,
		SuspiciousWindow:    config.GetDurationFromSeconds(suspiciousConfig.Window),
		BlockDuration:       config.GetDurationFromSeconds(suspiciousConfig.BlockDuration),
		TrackingWindow:      config.GetDurationFromSeconds(suspiciousConfig.TrackingWindow),
	}
}

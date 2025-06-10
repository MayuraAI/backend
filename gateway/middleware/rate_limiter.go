package middleware

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"gateway/pkg/logger"
)

// TokenBucket represents a token bucket for rate limiting
type TokenBucket struct {
	Capacity   int       // Maximum number of tokens (burst size)
	Tokens     int       // Current number of tokens
	RefillRate float64   // Tokens added per second
	LastRefill time.Time // Last time tokens were added
	mutex      sync.RWMutex
}

// RateLimiter manages multiple token buckets
type RateLimiter struct {
	buckets    map[string]*TokenBucket
	mutex      sync.RWMutex
	cleanupTTL time.Duration
}

// RateLimitConfig holds rate limiting configuration
type RateLimitConfig struct {
	RequestsPerMinute int           // Overall rate limit per minute
	BurstSize         int           // Maximum burst size
	CleanupInterval   time.Duration // How often to clean up old buckets
	CleanupTTL        time.Duration // How long to keep inactive buckets
}

// Default rate limiting configuration
var defaultConfig = RateLimitConfig{
	RequestsPerMinute: 3,              // 3 requests per minute per user
	BurstSize:         2,               // Allow burst of 2 requests
	CleanupInterval:   1 * time.Minute,  // Clean up every 1 minute
	CleanupTTL:        1 * time.Minute, // Remove buckets inactive for 1 minute
}

// Global rate limiter instance
var globalRateLimiter *RateLimiter

// Initialize the rate limiter
func init() {
	globalRateLimiter = NewRateLimiter(defaultConfig)
}

// NewRateLimiter creates a new rate limiter with the given configuration
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		buckets:    make(map[string]*TokenBucket),
		cleanupTTL: config.CleanupTTL,
	}

	// Start cleanup routine
	go rl.cleanupRoutine(config.CleanupInterval)

	return rl
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(capacity int, refillRatePerSecond float64) *TokenBucket {
	return &TokenBucket{
		Capacity:   capacity,
		Tokens:     capacity,
		RefillRate: refillRatePerSecond,
		LastRefill: time.Now(),
	}
}

// AllowRequest checks if a request should be allowed and consumes a token if so
func (tb *TokenBucket) AllowRequest() bool {
	tb.mutex.Lock()
	defer tb.mutex.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.LastRefill).Seconds()

	// Calculate tokens to add based on elapsed time
	tokensToAdd := elapsed * tb.RefillRate

	if tokensToAdd > 0 {
		// Add tokens but don't exceed capacity
		newTokens := float64(tb.Tokens) + tokensToAdd
		tb.Tokens = min(tb.Capacity, int(newTokens))
		tb.LastRefill = now
	}

	// Check if we have tokens available
	if tb.Tokens > 0 {
		tb.Tokens--
		return true
	}

	return false
}

// GetOrCreateBucket gets or creates a token bucket for the given key
func (rl *RateLimiter) GetOrCreateBucket(key string, config RateLimitConfig) *TokenBucket {
	rl.mutex.RLock()
	bucket, exists := rl.buckets[key]
	rl.mutex.RUnlock()

	if exists {
		return bucket
	}

	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	if bucket, exists := rl.buckets[key]; exists {
		return bucket
	}

	// Calculate refill rate: requests per minute -> tokens per second
	refillRate := float64(config.RequestsPerMinute) / 60.0
	bucket = NewTokenBucket(config.BurstSize, refillRate)
	rl.buckets[key] = bucket
	return bucket
}

// cleanupRoutine periodically removes old buckets to prevent memory leaks
func (rl *RateLimiter) cleanupRoutine(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for range ticker.C {
		rl.cleanup()
	}
}

// cleanup removes old inactive buckets
func (rl *RateLimiter) cleanup() {
	rl.mutex.Lock()
	defer rl.mutex.Unlock()

	now := time.Now()
	for key, bucket := range rl.buckets {
		bucket.mutex.RLock()
		inactive := now.Sub(bucket.LastRefill) > rl.cleanupTTL
		bucket.mutex.RUnlock()

		if inactive {
			delete(rl.buckets, key)
		}
	}
}

// GetBucketStats returns current bucket statistics
func (rl *RateLimiter) GetBucketStats() map[string]interface{} {
	rl.mutex.RLock()
	defer rl.mutex.RUnlock()

	return map[string]interface{}{
		"active_buckets": len(rl.buckets),
		"cleanup_ttl":    rl.cleanupTTL.String(),
	}
}

// RateLimitMiddleware creates a rate limiting middleware
func RateLimitMiddleware(config ...RateLimitConfig) func(http.Handler) http.Handler {
	// Use provided config or default
	cfg := defaultConfig
	if len(config) > 0 {
		cfg = config[0]
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log := logger.GetLogger("rate_limiter")

			// Create rate limit key based on user ID (from auth) or IP address
			key := getRateLimitKey(r)

			// Get or create bucket for this key
			bucket := globalRateLimiter.GetOrCreateBucket(key, cfg)

			// Check if request is allowed
			if !bucket.AllowRequest() {
				log.WarnWithFields("Rate limit exceeded", map[string]interface{}{
					"key":        key,
					"ip":         getClientIP(r),
					"user_agent": r.UserAgent(),
					"path":       r.URL.Path,
				})

				// Return rate limit error in streaming format
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error": "Rate limit exceeded. Please try again later.", "status": 429}`))
				return
			}

			// Add rate limit headers
			bucket.mutex.RLock()
			remaining := bucket.Tokens
			bucket.mutex.RUnlock()

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(cfg.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))

			// Continue to next handler
			next.ServeHTTP(w, r)
		})
	}
}

// getRateLimitKey generates a key for rate limiting based on user ID or IP
func getRateLimitKey(r *http.Request) string {
	// Try to get user ID from context (set by auth middleware)
	if user, ok := GetSupabaseUserFromContext(r.Context()); ok && user != nil {
		return "user:" + user.ID.String()
	}

	// Fall back to IP address
	return "ip:" + getClientIP(r)
}

// getClientIP extracts the real client IP address
func getClientIP(r *http.Request) string {
	// Check for common proxy headers
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		// X-Forwarded-For can contain multiple IPs, take the first one
		if idx := len(ip); idx > 0 {
			if commaIdx := 0; commaIdx < idx {
				for i, char := range ip {
					if char == ',' {
						commaIdx = i
						break
					}
				}
				if commaIdx > 0 {
					return ip[:commaIdx]
				}
			}
			return ip
		}
	}

	if ip := r.Header.Get("X-Real-IP"); ip != "" {
		return ip
	}

	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		return ip
	}

	// Fall back to RemoteAddr
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return ip
	}

	return r.RemoteAddr
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// GetRateLimitStats returns current rate limiter statistics
func GetRateLimitStats() map[string]interface{} {
	return globalRateLimiter.GetBucketStats()
}

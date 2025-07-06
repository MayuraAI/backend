package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// SubscriptionTier represents the different subscription tiers
type SubscriptionTier string

const (
	TierAnonymous SubscriptionTier = "anonymous"
	TierFree      SubscriptionTier = "free"
	TierPlus      SubscriptionTier = "plus"
	TierPro       SubscriptionTier = "pro"
)

// RateLimitConfig represents rate limiting configuration for a tier
type RateLimitConfig struct {
	FreeRequests      int  `json:"free_requests"`       // -1 means unlimited
	ProRequests       int  `json:"pro_requests"`        // -1 means unlimited
	RequestsPerDay    int  `json:"requests_per_day"`    // Total daily limit, -1 means unlimited
	DailyReset        bool `json:"daily_reset"`         // Whether to reset daily
	RequestsPerMinute int  `json:"requests_per_minute"` // Per-minute rate limit
}

// SubscriptionConfig holds all tier configurations
type SubscriptionConfig struct {
	Anonymous RateLimitConfig `json:"anonymous"`
	Free      RateLimitConfig `json:"free"`
	Plus      RateLimitConfig `json:"plus"`
	Pro       RateLimitConfig `json:"pro"`
}

// Default configuration
var defaultSubscriptionConfig = SubscriptionConfig{
	Anonymous: RateLimitConfig{
		FreeRequests:      5,     // Total 5 requests, not per day
		ProRequests:       0,     // No pro requests
		RequestsPerDay:    5,     // Total limit
		DailyReset:        false, // No daily reset for anonymous
		RequestsPerMinute: 3,     // Rate limit per minute
	},
	Free: RateLimitConfig{
		FreeRequests:      -1,   // Unlimited free requests
		ProRequests:       0,    // No pro requests
		RequestsPerDay:    -1,   // Unlimited daily
		DailyReset:        true, // Reset daily at midnight
		RequestsPerMinute: 5,    // Rate limit per minute
	},
	Plus: RateLimitConfig{
		FreeRequests:      -1,   // Unlimited free requests
		ProRequests:       50,   // 50 pro requests per day
		RequestsPerDay:    50,   // Total limit for pro requests
		DailyReset:        true, // Reset daily at midnight
		RequestsPerMinute: 10,   // Rate limit per minute
	},
	Pro: RateLimitConfig{
		FreeRequests:      -1,   // Unlimited free requests
		ProRequests:       100,  // 100 pro requests per day
		RequestsPerDay:    100,  // Total limit for pro requests
		DailyReset:        true, // Reset daily at midnight
		RequestsPerMinute: 15,   // Rate limit per minute
	},
}

// Global config instance
var subscriptionConfig *SubscriptionConfig

// LoadSubscriptionConfig loads configuration from environment or uses defaults
func LoadSubscriptionConfig() (*SubscriptionConfig, error) {
	if subscriptionConfig != nil {
		return subscriptionConfig, nil
	}

	// Try to load from config file first
	configFile := os.Getenv("SUBSCRIPTION_CONFIG_FILE")
	if configFile != "" {
		if config, err := loadFromFile(configFile); err == nil {
			subscriptionConfig = config
			return subscriptionConfig, nil
		}
	}

	// Use default configuration
	subscriptionConfig = &defaultSubscriptionConfig
	return subscriptionConfig, nil
}

// loadFromFile loads configuration from a JSON file
func loadFromFile(filename string) (*SubscriptionConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config SubscriptionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

// GetRateLimitConfig returns rate limit configuration for a tier
func GetRateLimitConfig(tier SubscriptionTier) (RateLimitConfig, error) {
	config, err := LoadSubscriptionConfig()
	if err != nil {
		return RateLimitConfig{}, err
	}

	switch tier {
	case TierAnonymous:
		return config.Anonymous, nil
	case TierFree:
		return config.Free, nil
	case TierPlus:
		return config.Plus, nil
	case TierPro:
		return config.Pro, nil
	default:
		return config.Free, nil // Default to free tier
	}
}

// IsUnlimited checks if a limit is unlimited (-1)
func IsUnlimited(limit int) bool {
	return limit == -1
}

// ValidateSubscriptionTier validates if a tier is valid
func ValidateSubscriptionTier(tier string) SubscriptionTier {
	switch SubscriptionTier(tier) {
	case TierAnonymous, TierFree, TierPlus, TierPro:
		return SubscriptionTier(tier)
	default:
		return TierFree // Default to free for invalid tiers
	}
}

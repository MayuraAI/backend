package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"
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
	FreeRequests      int  `json:"free_requests" yaml:"free_requests"`             // Free requests per day (-1 means unlimited)
	ProRequests       int  `json:"pro_requests" yaml:"pro_requests"`               // Pro requests per day (-1 means unlimited)
	RequestsPerDay    int  `json:"requests_per_day" yaml:"requests_per_day"`       // Total daily limit for pro requests
	DailyReset        bool `json:"daily_reset" yaml:"daily_reset"`                 // Whether to reset daily
	RequestsPerMinute int  `json:"requests_per_minute" yaml:"requests_per_minute"` // Per-minute rate limit
	LifetimeLimit     bool `json:"lifetime_limit" yaml:"lifetime_limit"`           // Whether this is a lifetime limit (for anonymous)
}

// SuspiciousActivityConfig represents suspicious activity detection settings
type SuspiciousActivityConfig struct {
	Threshold      int `json:"threshold" yaml:"threshold"`             // Max requests in window before blocking
	Window         int `json:"window" yaml:"window"`                   // Time window in seconds
	BlockDuration  int `json:"block_duration" yaml:"block_duration"`   // Block duration in seconds
	TrackingWindow int `json:"tracking_window" yaml:"tracking_window"` // Tracking window in seconds
}

// CleanupConfig represents cleanup settings
type CleanupConfig struct {
	Interval int `json:"interval" yaml:"interval"` // Cleanup interval in seconds
	TTL      int `json:"ttl" yaml:"ttl"`           // TTL in seconds
}

// SubscriptionConfig holds all tier configurations
type SubscriptionConfig struct {
	Anonymous          RateLimitConfig          `json:"anonymous" yaml:"anonymous"`
	Free               RateLimitConfig          `json:"free" yaml:"free"`
	Plus               RateLimitConfig          `json:"plus" yaml:"plus"`
	Pro                RateLimitConfig          `json:"pro" yaml:"pro"`
	SuspiciousActivity SuspiciousActivityConfig `json:"suspicious_activity" yaml:"suspicious_activity"`
	Cleanup            CleanupConfig            `json:"cleanup" yaml:"cleanup"`
}

// Default configuration matching the user's requirements
var defaultSubscriptionConfig = SubscriptionConfig{
	Anonymous: RateLimitConfig{
		FreeRequests:      5,     // 5 free requests total (lifetime)
		ProRequests:       0,     // 0 pro requests
		RequestsPerDay:    5,     // Total lifetime limit
		DailyReset:        false, // No daily reset for anonymous (lifetime limit)
		RequestsPerMinute: 3,     // Rate limit per minute
		LifetimeLimit:     true,  // This is a lifetime limit, not daily
	},
	Free: RateLimitConfig{
		FreeRequests:      100,   // 100 free requests per day
		ProRequests:       0,     // 0 pro requests per day
		RequestsPerDay:    100,   // Total daily limit for free requests
		DailyReset:        true,  // Reset daily at midnight
		RequestsPerMinute: 5,     // Rate limit per minute
		LifetimeLimit:     false, // Daily limit, not lifetime
	},
	Plus: RateLimitConfig{
		FreeRequests:      -1,    // Unlimited free requests
		ProRequests:       50,    // 50 pro requests per day
		RequestsPerDay:    50,    // Total daily limit for pro requests
		DailyReset:        true,  // Reset daily at midnight
		RequestsPerMinute: 10,    // Rate limit per minute
		LifetimeLimit:     false, // Daily limit, not lifetime
	},
	Pro: RateLimitConfig{
		FreeRequests:      -1,    // Unlimited free requests
		ProRequests:       100,   // 100 pro requests per day
		RequestsPerDay:    100,   // Total daily limit for pro requests
		DailyReset:        true,  // Reset daily at midnight
		RequestsPerMinute: 15,    // Rate limit per minute
		LifetimeLimit:     false, // Daily limit, not lifetime
	},
	SuspiciousActivity: SuspiciousActivityConfig{
		Threshold:      15,   // Max requests in window before blocking
		Window:         300,  // 5 minutes in seconds
		BlockDuration:  3600, // 1 hour in seconds
		TrackingWindow: 600,  // 10 minutes in seconds
	},
	Cleanup: CleanupConfig{
		Interval: 86400,  // 24 hours in seconds
		TTL:      172800, // 48 hours in seconds
	},
}

// Global config instance
var subscriptionConfig *SubscriptionConfig

// LoadSubscriptionConfig loads configuration from environment or uses defaults
func LoadSubscriptionConfig() (*SubscriptionConfig, error) {
	if subscriptionConfig != nil {
		return subscriptionConfig, nil
	}

	// Try to load from YAML config file first
	configFile := os.Getenv("SUBSCRIPTION_CONFIG_FILE")
	if configFile == "" {
		// Default to config file in same directory
		configFile = filepath.Join("config", "rate_limit_config.yaml")
	}

	if _, err := os.Stat(configFile); err == nil {
		if config, err := loadFromYAMLFile(configFile); err == nil {
			subscriptionConfig = config
			return subscriptionConfig, nil
		}
	}

	// Try to load from JSON config file as fallback
	jsonConfigFile := os.Getenv("SUBSCRIPTION_JSON_CONFIG_FILE")
	if jsonConfigFile != "" {
		if config, err := loadFromJSONFile(jsonConfigFile); err == nil {
			subscriptionConfig = config
			return subscriptionConfig, nil
		}
	}

	// Use default configuration
	subscriptionConfig = &defaultSubscriptionConfig
	return subscriptionConfig, nil
}

// loadFromYAMLFile loads configuration from a YAML file
func loadFromYAMLFile(filename string) (*SubscriptionConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML config file: %w", err)
	}

	var config SubscriptionConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse YAML config file: %w", err)
	}

	return &config, nil
}

// loadFromJSONFile loads configuration from a JSON file (legacy support)
func loadFromJSONFile(filename string) (*SubscriptionConfig, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read JSON config file: %w", err)
	}

	var config SubscriptionConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse JSON config file: %w", err)
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

// GetSuspiciousActivityConfig returns suspicious activity configuration
func GetSuspiciousActivityConfig() (SuspiciousActivityConfig, error) {
	config, err := LoadSubscriptionConfig()
	if err != nil {
		return SuspiciousActivityConfig{}, err
	}
	return config.SuspiciousActivity, nil
}

// GetCleanupConfig returns cleanup configuration
func GetCleanupConfig() (CleanupConfig, error) {
	config, err := LoadSubscriptionConfig()
	if err != nil {
		return CleanupConfig{}, err
	}
	return config.Cleanup, nil
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

// GetUserTier determines the user's subscription tier
func GetUserTier(isAnonymous bool, subscriptionTier string) SubscriptionTier {
	if isAnonymous {
		return TierAnonymous
	}
	return ValidateSubscriptionTier(subscriptionTier)
}

// GetDurationFromSeconds converts seconds to time.Duration
func GetDurationFromSeconds(seconds int) time.Duration {
	return time.Duration(seconds) * time.Second
}

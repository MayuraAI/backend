package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"gateway/config"
	"gateway/pkg/logger"
	"net/http"
	"os"
	"time"
)

// UserTierResponse represents the response from the subscription service
type UserTierResponse struct {
	UserID string                  `json:"user_id"`
	Tier   config.SubscriptionTier `json:"tier"`
	Status string                  `json:"status"`
}

// GetUserTierFromSubscriptionService gets the user's tier from the subscription service
func GetUserTierFromSubscriptionService(ctx context.Context, userID string, authToken string) (config.SubscriptionTier, error) {
	// Get payment service URL from environment
	paymentServiceURL := os.Getenv("PAYMENT_SERVICE_URL")
	if paymentServiceURL == "" {
		paymentServiceURL = "http://localhost:8081" // Default payment service URL
	}

	// Create HTTP request to get subscription status
	url := fmt.Sprintf("%s/api/subscription/status/%s", paymentServiceURL, userID)

	client := &http.Client{Timeout: 2 * time.Second} // Short timeout for rate limiting
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return config.TierFree, fmt.Errorf("failed to create request: %w", err)
	}

	// Set authorization header
	req.Header.Set("Authorization", authToken)
	req.Header.Set("Content-Type", "application/json")

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		// Log warning but don't fail the rate limiting - default to free tier
		logger.GetDailyLogger().Warn("Failed to get user tier from subscription service: %v", err)
		return config.TierFree, nil
	}
	defer resp.Body.Close()

	// Handle different response statuses
	switch resp.StatusCode {
	case http.StatusOK:
		// Parse the response
		var tierResponse UserTierResponse
		if err := json.NewDecoder(resp.Body).Decode(&tierResponse); err != nil {
			logger.GetDailyLogger().Warn("Failed to decode subscription response: %v", err)
			return config.TierFree, nil
		}

		// Validate and return the tier
		validatedTier := config.ValidateSubscriptionTier(string(tierResponse.Tier))
		logger.GetDailyLogger().Info("Got user tier from subscription service", "user_id", userID, "tier", validatedTier)
		return validatedTier, nil

	case http.StatusNotFound:
		// User not found in subscription service - default to free tier
		logger.GetDailyLogger().Info("User not found in subscription service, defaulting to free tier", "user_id", userID)
		return config.TierFree, nil

	default:
		// Other errors - log and default to free tier
		logger.GetDailyLogger().Warn("Subscription service returned status %d, defaulting to free tier", resp.StatusCode)
		return config.TierFree, nil
	}
}

// GetUserTierFromContext determines the user's tier from context with subscription service lookup
func GetUserTierFromContext(ctx context.Context, r *http.Request) (config.SubscriptionTier, bool) {
	// Get user from context
	user, userOk := GetFirebaseUserFromContext(ctx)
	if !userOk || user == nil {
		// No user in context - anonymous
		return config.TierAnonymous, true
	}

	// Check if user is anonymous
	if IsAnonymousUser(user) {
		return config.TierAnonymous, true
	}

	// For authenticated users, try to get tier from subscription service
	authHeader := r.Header.Get("Authorization")
	if authHeader != "" {
		if tier, err := GetUserTierFromSubscriptionService(ctx, user.UID, authHeader); err == nil {
			return tier, false
		}
	}

	// Fallback to free tier for authenticated users
	logger.GetDailyLogger().Info("Falling back to free tier for authenticated user", "user_id", user.UID)
	return config.TierFree, false
}

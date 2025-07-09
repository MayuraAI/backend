package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"gateway/config"
	"gateway/middleware"

	"github.com/redis/go-redis/v9"
)

// SubscriptionHandler handles subscription-related requests
type SubscriptionHandler struct {
	RedisClient   *redis.Client
	PaymentAPIURL string
}

// UserSubscriptionResponse represents the subscription status response
type UserSubscriptionResponse struct {
	UserID         string                  `json:"user_id"`
	SubscriptionID *string                 `json:"sub_id,omitempty"`
	Tier           config.SubscriptionTier `json:"tier"`
	Status         string                  `json:"status"`
	ExpiresAt      *time.Time              `json:"expires_at,omitempty"`
	RateLimit      config.RateLimitConfig  `json:"rate_limit"`
	Usage          *UserUsageStats         `json:"usage,omitempty"`
}

// UserUsageStats represents current usage statistics
type UserUsageStats struct {
	FreeRequestsUsed int       `json:"free_requests_used"`
	MaxRequestsUsed  int       `json:"max_requests_used"`
	LastReset        time.Time `json:"last_reset"`
}

// NewSubscriptionHandler creates a new subscription handler
func NewSubscriptionHandler(redisClient *redis.Client, paymentAPIURL string) *SubscriptionHandler {
	return &SubscriptionHandler{
		RedisClient:   redisClient,
		PaymentAPIURL: paymentAPIURL,
	}
}

// GetUserSubscription retrieves user subscription information
func (h *SubscriptionHandler) GetUserSubscription(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by Firebase auth middleware)
	userID, ok := r.Context().Value("authenticated_user_id").(string)
	if !ok {
		sendAPIErrorResponse(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Get subscription info directly from payment service
	subscriptionInfo, err := h.getSubscriptionFromPaymentService(userID)
	if err != nil {
		log.Printf("Failed to get user subscription for user %s: %v", userID, err)
		sendAPIErrorResponse(w, "Failed to retrieve subscription information", http.StatusInternalServerError)
		return
	}

	// Get current usage stats
	usage, err := h.getUserUsageStats(userID, subscriptionInfo.Tier)
	if err != nil {
		log.Printf("Failed to get usage stats for user %s: %v", userID, err)
		// Continue without usage stats
	}

	subscriptionInfo.Usage = usage

	sendJSONResponse(w, subscriptionInfo, http.StatusOK)
}

// getSubscriptionFromPaymentService calls the payment service to get subscription info
func (h *SubscriptionHandler) getSubscriptionFromPaymentService(userID string) (*UserSubscriptionResponse, error) {
	// Make HTTP request to payment service
	url := fmt.Sprintf("%s/api/subscription/status/%s", h.PaymentAPIURL, userID)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		// Fallback to free tier if payment service is down
		log.Printf("Payment service unavailable, defaulting to free tier for user %s: %v", userID, err)
		return h.createDefaultSubscription(userID), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// User not found in payment service, return free tier
		return h.createDefaultSubscription(userID), nil
	}

	if resp.StatusCode != http.StatusOK {
		// Fallback to free tier if payment service returns an error
		log.Printf("Payment service returned status %d, defaulting to free tier for user %s", resp.StatusCode, userID)
		return h.createDefaultSubscription(userID), nil
	}

	var paymentResponse struct {
		UserID         string     `json:"user_id"`
		SubscriptionID *string    `json:"sub_id"`
		Tier           string     `json:"tier"`
		Status         string     `json:"status"`
		ExpiresAt      *time.Time `json:"expires_at"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&paymentResponse); err != nil {
		return nil, fmt.Errorf("failed to decode payment response: %w", err)
	}

	// Validate tier and status
	tier := config.ValidateSubscriptionTier(paymentResponse.Tier)

	// Check if subscription is expired or cancelled
	if paymentResponse.ExpiresAt != nil && time.Now().After(*paymentResponse.ExpiresAt) {
		tier = config.TierFree
	}

	// Get rate limit config for the tier
	rateLimit, err := config.GetRateLimitConfig(tier)
	if err != nil {
		return nil, fmt.Errorf("failed to get rate limit config: %w", err)
	}

	return &UserSubscriptionResponse{
		UserID:         userID,
		SubscriptionID: paymentResponse.SubscriptionID,
		Tier:           tier,
		Status:         paymentResponse.Status,
		ExpiresAt:      paymentResponse.ExpiresAt,
		RateLimit:      rateLimit,
	}, nil
}

// createDefaultSubscription creates a default free tier subscription
func (h *SubscriptionHandler) createDefaultSubscription(userID string) *UserSubscriptionResponse {
	rateLimit, _ := config.GetRateLimitConfig(config.TierFree)

	return &UserSubscriptionResponse{
		UserID:         userID,
		SubscriptionID: nil,
		Tier:           config.TierFree,
		Status:         "active",
		ExpiresAt:      nil,
		RateLimit:      rateLimit,
	}
}

// getUserUsageStats retrieves current usage statistics using the new rate limiting system
func (h *SubscriptionHandler) getUserUsageStats(userID string, tier config.SubscriptionTier) (*UserUsageStats, error) {
	// Get usage key based on tier
	var usageKey string
	var isAnonymous bool

	if tier == config.TierAnonymous {
		usageKey = fmt.Sprintf("anonymous:%s", userID)
		isAnonymous = true
	} else {
		usageKey = fmt.Sprintf("user:%s", userID)
		isAnonymous = false
	}

	// Get usage info from the rate limiting system
	ctx := context.Background()
	freeCount, proCount, resetTime, _, _, err := middleware.GetUsageInfo(ctx, usageKey, tier, isAnonymous)
	if err != nil {
		return nil, fmt.Errorf("failed to get usage data: %w", err)
	}

	usage := &UserUsageStats{
		FreeRequestsUsed: freeCount,
		MaxRequestsUsed:  proCount,
		LastReset:        resetTime,
	}

	return usage, nil
}

// CreateCheckoutSession creates a new checkout session
func (h *SubscriptionHandler) CreateCheckoutSession(w http.ResponseWriter, r *http.Request) {
	// Verify user is authenticated (Firebase auth middleware has already verified the token)
	_, ok := r.Context().Value("authenticated_user_id").(string)
	if !ok {
		sendAPIErrorResponse(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	var request struct {
		Tier string `json:"tier"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		sendAPIErrorResponse(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate tier
	tier := config.ValidateSubscriptionTier(request.Tier)
	if tier == config.TierFree || tier == config.TierAnonymous {
		sendAPIErrorResponse(w, "Invalid subscription tier", http.StatusBadRequest)
		return
	}

	// Get the Authorization header from the original request
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		sendAPIErrorResponse(w, "Missing authorization header", http.StatusUnauthorized)
		return
	}

	// Forward to payment service
	url := fmt.Sprintf("%s/api/checkout", h.PaymentAPIURL)

	paymentRequest := map[string]interface{}{
		"tier": string(tier),
	}

	paymentData, _ := json.Marshal(paymentRequest)

	// Create HTTP request with Authorization header
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(paymentData))
	if err != nil {
		log.Printf("Failed to create checkout request: %v", err)
		sendAPIErrorResponse(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authHeader)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to create checkout session: %v", err)
		sendAPIErrorResponse(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Handle different status codes from payment service
		switch resp.StatusCode {
		case http.StatusConflict:
			// User already has an active subscription to this tier
			var errorResponse struct {
				Error       string `json:"error"`
				CurrentTier string `json:"current_tier,omitempty"`
				Status      string `json:"status,omitempty"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err == nil {
				sendJSONResponse(w, errorResponse, http.StatusConflict)
				return
			}
			sendAPIErrorResponse(w, "Already subscribed to this tier", http.StatusConflict)
		case http.StatusBadRequest:
			sendAPIErrorResponse(w, "Invalid subscription request", http.StatusBadRequest)
		case http.StatusUnauthorized:
			sendAPIErrorResponse(w, "Authentication failed", http.StatusUnauthorized)
		default:
			sendAPIErrorResponse(w, "Payment service error", http.StatusInternalServerError)
		}
		return
	}

	var checkoutResponse struct {
		CheckoutURL string `json:"checkout_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&checkoutResponse); err != nil {
		sendAPIErrorResponse(w, "Failed to decode checkout response", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, checkoutResponse, http.StatusOK)
}

// GetManagementURL gets the subscription management URL for the authenticated user
func (h *SubscriptionHandler) GetManagementURL(w http.ResponseWriter, r *http.Request) {
	// Get user ID from context (set by Firebase auth middleware)
	userID, ok := r.Context().Value("authenticated_user_id").(string)
	if !ok {
		sendAPIErrorResponse(w, "User not authenticated", http.StatusUnauthorized)
		return
	}

	// Get the Authorization header from the original request
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		sendAPIErrorResponse(w, "Missing authorization header", http.StatusUnauthorized)
		return
	}

	// Forward to payment service
	url := fmt.Sprintf("%s/api/subscription/management/%s", h.PaymentAPIURL, userID)

	// Create HTTP request with Authorization header
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Printf("Failed to create management URL request: %v", err)
		sendAPIErrorResponse(w, "Failed to get management URL", http.StatusInternalServerError)
		return
	}

	// Set headers
	req.Header.Set("Authorization", authHeader)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Failed to get management URL: %v", err)
		sendAPIErrorResponse(w, "Failed to get management URL", http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		// Try to get the specific error message from the payment service
		var errorResponse struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&errorResponse); err == nil && errorResponse.Error != "" {
			sendAPIErrorResponse(w, errorResponse.Error, http.StatusNotFound)
		} else {
			sendAPIErrorResponse(w, "Active subscription not found", http.StatusNotFound)
		}
		return
	}

	if resp.StatusCode != http.StatusOK {
		sendAPIErrorResponse(w, "Payment service error", http.StatusInternalServerError)
		return
	}

	var managementResponse struct {
		ManagementURL string `json:"management_url"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&managementResponse); err != nil {
		sendAPIErrorResponse(w, "Failed to decode management response", http.StatusInternalServerError)
		return
	}

	sendJSONResponse(w, managementResponse, http.StatusOK)
}

package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"payment/dynamo"
	"time"

	"github.com/gin-gonic/gin"
)

// SubscriptionStatusResponse represents the response for subscription status
type SubscriptionStatusResponse struct {
	UserID         string     `json:"user_id"`
	SubscriptionID *string    `json:"sub_id,omitempty"`
	Tier           string     `json:"tier"`
	Status         string     `json:"status"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// GetSubscriptionStatusHandler returns the subscription status for a user
func GetSubscriptionStatusHandler(c *gin.Context) {
	startTime := time.Now()
	userID := c.Param("user_id")
	requestID := fmt.Sprintf("subscription-status-%d", startTime.UnixNano())

	log.Printf("📊 [%s] Get subscription status request for user: %s", requestID, userID)

	if userID == "" {
		log.Printf("❌ [%s] User ID is required", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	// Get subscription from DynamoDB
	subscription, err := dynamo.GetSubscription(context.Background(), userID)
	if err != nil {
		log.Printf("❌ [%s] Error getting subscription for user %s: %v", requestID, userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	// If subscription not found, return 404
	if subscription == nil {
		log.Printf("❌ [%s] Subscription not found for user: %s", requestID, userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Subscription not found"})
		return
	}

	// Check if subscription is expired
	if subscription.ExpiresAt != nil && time.Now().After(*subscription.ExpiresAt) {
		log.Printf("⏰ [%s] Subscription expired for user: %s", requestID, userID)
		subscription.Status = "expired"
		subscription.Tier = "free"
	}

	// Convert to response format
	response := &SubscriptionStatusResponse{
		UserID:         subscription.UserID,
		SubscriptionID: &subscription.SubID,
		Tier:           subscription.Tier,
		Status:         subscription.Status,
		ExpiresAt:      subscription.ExpiresAt,
		CreatedAt:      subscription.CreatedAt,
		UpdatedAt:      subscription.UpdatedAt,
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Subscription status response sent in %v", requestID, duration)

	c.JSON(http.StatusOK, response)
}

// GetUserManagementURLHandler returns the LemonSqueezy management URL for a user
func GetUserManagementURLHandler(c *gin.Context) {
	startTime := time.Now()
	userID := c.Param("user_id")
	requestID := fmt.Sprintf("management-url-%d", startTime.UnixNano())

	log.Printf("🔗 [%s] Get management URL request for user: %s", requestID, userID)

	if userID == "" {
		log.Printf("❌ [%s] User ID is required", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	// Get subscription from DynamoDB
	subscription, err := dynamo.GetSubscription(context.Background(), userID)
	if err != nil {
		log.Printf("❌ [%s] Error getting subscription for user %s: %v", requestID, userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	log.Printf("🔍 [%s] Subscription: %+v", requestID, subscription)

	// If no subscription found, return error
	if subscription == nil {
		log.Printf("❌ [%s] No subscription found for user: %s", requestID, userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "No subscription found"})
		return
	}

	// Check if subscription is active
	if subscription.Status != "active" {
		log.Printf("❌ [%s] Subscription not active for user: %s, status: %s", requestID, userID, subscription.Status)
		c.JSON(http.StatusNotFound, gin.H{"error": "No active subscription found"})
		return
	}

	// Check if subscription has a LemonSqueezy subscription ID
	if subscription.SubID == "" {
		log.Printf("❌ [%s] Subscription missing LemonSqueezy ID for user: %s", requestID, userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Management URL not available - subscription created outside of LemonSqueezy"})
		return
	}

	// Use the customer portal URL stored in the subscription
	managementURL := subscription.CustomerPortalURL
	if managementURL == "" {
		log.Printf("❌ [%s] No customer portal URL found for subscription %s", requestID, subscription.SubID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Management URL not available"})
		return
	}

	response := map[string]string{
		"management_url": managementURL,
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Management URL response sent in %v", requestID, duration)

	c.JSON(http.StatusOK, response)
}

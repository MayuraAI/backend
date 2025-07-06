package handlers

import (
	"context"
	"net/http"
	"strings"

	"payment/dynamo"
	"payment/firebase"
	"payment/lsz"

	"github.com/gin-gonic/gin"
)

// TierResponse represents the response for tier status
type TierResponse struct {
	Tier      string `json:"tier"`
	Status    string `json:"status,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	UserID    string `json:"user_id,omitempty"`
}

// GetUserTierHandler handles GET /api/tier
func GetUserTierHandler(c *gin.Context) {
	// Extract Firebase ID token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
		return
	}

	// Check if the header has the Bearer prefix
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Verify the Firebase ID token
	uid, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		return
	}

	// Get subscription from database
	sub, err := dynamo.GetSubscription(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription"})
		return
	}

	// Default response for free tier
	response := TierResponse{
		Tier:   "free",
		UserID: uid,
	}

	// If subscription exists and is active, return the tier info
	if sub != nil && lsz.IsValidSubscriptionStatus(sub.Status) {
		response.Tier = sub.Tier
		response.Status = sub.Status
		if sub.ExpiresAt != nil {
			response.ExpiresAt = sub.ExpiresAt.Format("2006-01-02T15:04:05Z")
		}
	}

	c.JSON(http.StatusOK, response)
}

// GetSubscriptionDetailsHandler handles GET /api/subscription
func GetSubscriptionDetailsHandler(c *gin.Context) {
	// Extract Firebase ID token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
		return
	}

	// Check if the header has the Bearer prefix
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Verify the Firebase ID token
	uid, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		return
	}

	// Get subscription from database
	sub, err := dynamo.GetSubscription(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription"})
		return
	}

	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No subscription found"})
		return
	}

	// Return full subscription details
	c.JSON(http.StatusOK, sub)
}

// HealthCheckHandler handles GET /health
func HealthCheckHandler(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status":  "healthy",
		"service": "payment",
		"message": "Payment service is running",
	})
}

// SubscriptionURLsResponse represents the response for subscription URLs
type SubscriptionURLsResponse struct {
	CustomerPortalURL                   string `json:"customer_portal_url,omitempty"`
	UpdatePaymentMethodURL              string `json:"update_payment_method_url,omitempty"`
	CustomerPortalUpdateSubscriptionURL string `json:"customer_portal_update_subscription_url,omitempty"`
}

// GetSubscriptionURLsHandler handles GET /api/subscription/urls
func GetSubscriptionURLsHandler(c *gin.Context) {
	// Extract Firebase ID token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
		return
	}

	// Check if the header has the Bearer prefix
	if !strings.HasPrefix(authHeader, "Bearer ") {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")

	// Verify the Firebase ID token
	uid, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		return
	}

	// Get subscription from database
	sub, err := dynamo.GetSubscription(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription"})
		return
	}

	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No subscription found"})
		return
	}

	// Return subscription URLs
	response := SubscriptionURLsResponse{
		CustomerPortalURL:                   sub.CustomerPortalURL,
		UpdatePaymentMethodURL:              sub.UpdatePaymentMethodURL,
		CustomerPortalUpdateSubscriptionURL: sub.CustomerPortalUpdateSubscriptionURL,
	}

	c.JSON(http.StatusOK, response)
}

package handlers

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"payment/dynamo"
	"payment/firebase"
	"payment/lsz"

	"github.com/gin-gonic/gin"
)

// CheckoutRequest represents the request body for creating a checkout
type CheckoutRequest struct {
	Tier      string `json:"tier" binding:"required"` // "plus" or "pro"
	VariantID int    `json:"variant_id"`              // Optional, will be determined from tier if not provided
}

// CheckoutResponse represents the response for checkout creation
type CheckoutResponse struct {
	CheckoutURL string `json:"checkout_url"`
	Message     string `json:"message,omitempty"`
}

// CreateCheckoutHandler handles POST /api/checkout
func CreateCheckoutHandler(c *gin.Context) {
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

	// Get user record to get email
	userRecord, err := firebase.GetUserRecord(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user information"})
		return
	}

	// Parse request body
	var req CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
		return
	}

	// Validate tier
	if req.Tier != "plus" && req.Tier != "pro" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tier. Must be 'plus' or 'pro'"})
		return
	}

	// Determine variant ID if not provided
	variantID := req.VariantID
	if variantID == 0 {
		variantID = lsz.GetTierVariantID(req.Tier)
		if variantID == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tier or variant ID not configured"})
			return
		}
	}

	// Check if user already has an active subscription
	existingSub, err := dynamo.GetSubscription(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing subscription"})
		return
	}

	// Check if user already has the same tier subscription
	if existingSub != nil && existingSub.Tier == req.Tier && lsz.IsValidSubscriptionStatus(existingSub.Status) {
		c.JSON(http.StatusConflict, gin.H{
			"error":        "Already subscribed to this tier",
			"current_tier": existingSub.Tier,
			"status":       existingSub.Status,
		})
		return
	}

	// Create checkout session with LemonSqueezy
	checkoutResp, err := lsz.CreateCheckout(variantID, uid, userRecord.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create checkout session", "details": err.Error()})
		return
	}

	// Return checkout URL
	response := CheckoutResponse{
		CheckoutURL: checkoutResp.Data.Attributes.URL,
		Message:     fmt.Sprintf("Checkout created for %s tier", req.Tier),
	}

	c.JSON(http.StatusOK, response)
}

// CancelSubscriptionHandler handles POST /api/cancel-subscription
func CancelSubscriptionHandler(c *gin.Context) {
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

	// Get current subscription
	sub, err := dynamo.GetSubscription(context.Background(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription"})
		return
	}

	if sub == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No active subscription found"})
		return
	}

	// Update subscription status to cancelled
	sub.Status = "cancelled"
	err = dynamo.SaveSubscriptionDetailed(context.Background(), *sub)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update subscription"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Subscription cancelled successfully",
		"tier":    "free",
	})
}

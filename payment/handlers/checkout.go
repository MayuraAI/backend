package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

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
	startTime := time.Now()
	requestID := fmt.Sprintf("checkout-%d", startTime.UnixNano())

	log.Printf("💳 [%s] Create checkout request started", requestID)
	log.Printf("💳 [%s] Client IP: %s", requestID, c.ClientIP())
	log.Printf("💳 [%s] Headers: %+v", requestID, c.Request.Header)

	// Extract Firebase ID token from Authorization header
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		log.Printf("❌ [%s] No authorization header provided", requestID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
		return
	}

	// Check if the header has the Bearer prefix
	if !strings.HasPrefix(authHeader, "Bearer ") {
		log.Printf("❌ [%s] Invalid authorization header format", requestID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
		return
	}

	idToken := strings.TrimPrefix(authHeader, "Bearer ")
	log.Printf("🔐 [%s] Authorization header present", requestID)

	// Verify the Firebase ID token
	log.Printf("🔥 [%s] Verifying Firebase token...", requestID)
	uid, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		log.Printf("❌ [%s] Firebase token verification failed: %v", requestID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token"})
		return
	}

	log.Printf("✅ [%s] Firebase token verified for user: %s", requestID, uid)

	// Get user record to get email
	log.Printf("👤 [%s] Fetching user record from Firebase...", requestID)
	userRecord, err := firebase.GetUserRecord(context.Background(), uid)
	if err != nil {
		log.Printf("❌ [%s] Failed to get user record: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user information"})
		return
	}

	log.Printf("✅ [%s] User record fetched - Email: %s", requestID, userRecord.Email)

	// Parse request body
	log.Printf("📝 [%s] Parsing request body...", requestID)
	var req CheckoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("❌ [%s] Invalid request body: %v", requestID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Request body parsed:", requestID)
	log.Printf("   Tier: %s", req.Tier)
	log.Printf("   Variant ID: %d", req.VariantID)

	// Validate tier
	if req.Tier != "plus" && req.Tier != "pro" {
		log.Printf("❌ [%s] Invalid tier specified: %s", requestID, req.Tier)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tier. Must be 'plus' or 'pro'"})
		return
	}

	log.Printf("✅ [%s] Tier validation passed: %s", requestID, req.Tier)

	// Determine variant ID if not provided
	variantID := req.VariantID
	if variantID == 0 {
		log.Printf("🔍 [%s] No variant ID provided, determining from tier...", requestID)
		variantID = lsz.GetTierVariantID(req.Tier)
		if variantID == 0 {
			log.Printf("❌ [%s] Failed to determine variant ID for tier: %s", requestID, req.Tier)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid tier or variant ID not configured"})
			return
		}
		log.Printf("✅ [%s] Variant ID determined: %d", requestID, variantID)
	} else {
		log.Printf("✅ [%s] Using provided variant ID: %d", requestID, variantID)
	}

	// Check if user already has an active subscription
	log.Printf("🔍 [%s] Checking for existing subscription...", requestID)
	existingSub, err := dynamo.GetSubscription(context.Background(), uid)
	// if err != nil {
	// 	log.Printf("❌ [%s] Failed to check existing subscription: %v", requestID, err)
	// 	c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check existing subscription"})
	// 	return
	// }

	if existingSub != nil {
		log.Printf("🔍 [%s] Existing subscription found:", requestID)
		log.Printf("   Current Tier: %s", existingSub.Tier)
		log.Printf("   Current Status: %s", existingSub.Status)
		log.Printf("   Variant ID: %d", existingSub.VariantID)

		// Check if user already has the same tier subscription
		if existingSub.Tier == req.Tier && lsz.IsValidSubscriptionStatus(existingSub.Status) {
			log.Printf("❌ [%s] User already has active subscription to tier: %s", requestID, req.Tier)
			c.JSON(http.StatusConflict, gin.H{
				"error":        "Already subscribed to this tier",
				"current_tier": existingSub.Tier,
				"status":       existingSub.Status,
			})
			return
		}

		log.Printf("✅ [%s] User can upgrade/change subscription from %s to %s", requestID, existingSub.Tier, req.Tier)
	} else {
		log.Printf("✅ [%s] No existing subscription found, proceeding with new checkout", requestID)
	}

	// Create checkout session with LemonSqueezy
	log.Printf("🍋 [%s] Creating LemonSqueezy checkout session...", requestID)
	log.Printf("   Variant ID: %d", variantID)
	log.Printf("   User ID: %s", uid)
	log.Printf("   Email: %s", userRecord.Email)

	checkoutResp, err := lsz.CreateCheckout(variantID, uid, userRecord.Email)
	if err != nil {
		log.Printf("❌ [%s] Failed to create checkout session: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create checkout session", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] LemonSqueezy checkout session created successfully", requestID)
	log.Printf("   Checkout URL: %s", checkoutResp.Data.Attributes.URL)

	// Return checkout URL
	response := CheckoutResponse{
		CheckoutURL: checkoutResp.Data.Attributes.URL,
		Message:     fmt.Sprintf("Checkout created for %s tier", req.Tier),
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Checkout response sent in %v", requestID, duration)

	c.JSON(http.StatusOK, response)
}

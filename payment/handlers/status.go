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

	"github.com/gin-gonic/gin"
)

// HealthCheckHandler handles GET /health
func HealthCheckHandler(c *gin.Context) {
	requestID := fmt.Sprintf("health-%d", time.Now().UnixNano())
	log.Printf("💗 [%s] Health check requested from %s", requestID, c.ClientIP())

	c.JSON(http.StatusOK, gin.H{
		"status":    "healthy",
		"service":   "payment",
		"timestamp": time.Now().Format(time.RFC3339),
		"version":   "1.0.0",
	})

	log.Printf("✅ [%s] Health check completed successfully", requestID)
}

// GetUserTierHandler handles GET /api/tier
func GetUserTierHandler(c *gin.Context) {
	startTime := time.Now()
	requestID := fmt.Sprintf("tier-%d", startTime.UnixNano())

	log.Printf("🎫 [%s] Get user tier request started", requestID)
	log.Printf("🎫 [%s] Client IP: %s", requestID, c.ClientIP())
	log.Printf("🎫 [%s] Headers: %+v", requestID, c.Request.Header)

	// Get the authorization header
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

	// Verify the Firebase token
	log.Printf("🔥 [%s] Verifying Firebase token...", requestID)
	userID, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		log.Printf("❌ [%s] Firebase token verification failed: %v", requestID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication token", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Firebase token verified for user: %s", requestID, userID)

	// Get user subscription
	log.Printf("🗄️ [%s] Fetching subscription for user %s", requestID, userID)
	ctx := context.Background()
	subscription, err := dynamo.GetSubscription(ctx, userID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get subscription from database: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription", "details": err.Error()})
		return
	}

	if subscription == nil {
		log.Printf("📋 [%s] No subscription found for user %s, returning free tier", requestID, userID)
		duration := time.Since(startTime)
		c.JSON(http.StatusOK, gin.H{
			"tier":            "free",
			"status":          "inactive",
			"request_id":      requestID,
			"processing_time": duration.String(),
		})
		log.Printf("✅ [%s] Tier response sent (free) in %v", requestID, duration)
		return
	}

	log.Printf("📋 [%s] Subscription found for user %s:", requestID, userID)
	log.Printf("   Tier: %s", subscription.Tier)
	log.Printf("   Status: %s", subscription.Status)
	log.Printf("   Variant ID: %d", subscription.VariantID)
	log.Printf("   Created: %s", subscription.CreatedAt.Format(time.RFC3339))
	log.Printf("   Updated: %s", subscription.UpdatedAt.Format(time.RFC3339))
	if subscription.ExpiresAt != nil {
		log.Printf("   Expires: %s", subscription.ExpiresAt.Format(time.RFC3339))
	}

	duration := time.Since(startTime)
	response := gin.H{
		"tier":            subscription.Tier,
		"status":          subscription.Status,
		"variant_id":      subscription.VariantID,
		"created_at":      subscription.CreatedAt.Format(time.RFC3339),
		"updated_at":      subscription.UpdatedAt.Format(time.RFC3339),
		"request_id":      requestID,
		"processing_time": duration.String(),
	}

	if subscription.ExpiresAt != nil {
		response["expires_at"] = subscription.ExpiresAt.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, response)
	log.Printf("✅ [%s] Tier response sent (%s) in %v", requestID, subscription.Tier, duration)
}

// GetSubscriptionDetailsHandler handles GET /api/subscription
func GetSubscriptionDetailsHandler(c *gin.Context) {
	startTime := time.Now()
	requestID := fmt.Sprintf("subscription-%d", startTime.UnixNano())

	log.Printf("📄 [%s] Get subscription details request started", requestID)
	log.Printf("📄 [%s] Client IP: %s", requestID, c.ClientIP())
	log.Printf("📄 [%s] Headers: %+v", requestID, c.Request.Header)

	// Get the authorization header
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

	// Verify the Firebase token
	log.Printf("🔥 [%s] Verifying Firebase token...", requestID)
	userID, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		log.Printf("❌ [%s] Firebase token verification failed: %v", requestID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication token", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Firebase token verified for user: %s", requestID, userID)

	// Get user subscription
	log.Printf("🗄️ [%s] Fetching subscription details for user %s", requestID, userID)
	ctx := context.Background()
	subscription, err := dynamo.GetSubscription(ctx, userID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get subscription from database: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription", "details": err.Error()})
		return
	}

	if subscription == nil {
		log.Printf("📋 [%s] No subscription found for user %s", requestID, userID)
		duration := time.Since(startTime)
		c.JSON(http.StatusOK, gin.H{
			"subscription":    nil,
			"message":         "No active subscription found",
			"request_id":      requestID,
			"processing_time": duration.String(),
		})
		log.Printf("✅ [%s] No subscription response sent in %v", requestID, duration)
		return
	}

	log.Printf("📋 [%s] Full subscription details for user %s:", requestID, userID)
	log.Printf("   UserID: %s", subscription.UserID)
	log.Printf("   Tier: %s", subscription.Tier)
	log.Printf("   Status: %s", subscription.Status)
	log.Printf("   Variant ID: %d", subscription.VariantID)
	log.Printf("   SubID: %s", subscription.SubID)
	log.Printf("   CustomerID: %s", subscription.CustomerID)
	log.Printf("   Email: %s", subscription.Email)
	log.Printf("   Created: %s", subscription.CreatedAt.Format(time.RFC3339))
	log.Printf("   Updated: %s", subscription.UpdatedAt.Format(time.RFC3339))
	if subscription.ExpiresAt != nil {
		log.Printf("   Expires: %s", subscription.ExpiresAt.Format(time.RFC3339))
	}

	duration := time.Since(startTime)
	response := gin.H{
		"subscription": gin.H{
			"user_id":     subscription.UserID,
			"tier":        subscription.Tier,
			"status":      subscription.Status,
			"variant_id":  subscription.VariantID,
			"sub_id":      subscription.SubID,
			"customer_id": subscription.CustomerID,
			"email":       subscription.Email,
			"created_at":  subscription.CreatedAt.Format(time.RFC3339),
			"updated_at":  subscription.UpdatedAt.Format(time.RFC3339),
		},
		"request_id":      requestID,
		"processing_time": duration.String(),
	}

	if subscription.ExpiresAt != nil {
		response["subscription"].(gin.H)["expires_at"] = subscription.ExpiresAt.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, response)
	log.Printf("✅ [%s] Subscription details response sent in %v", requestID, duration)
}

// GetSubscriptionURLsHandler handles GET /api/subscription/urls
func GetSubscriptionURLsHandler(c *gin.Context) {
	startTime := time.Now()
	requestID := fmt.Sprintf("urls-%d", startTime.UnixNano())

	log.Printf("🔗 [%s] Get subscription URLs request started", requestID)
	log.Printf("🔗 [%s] Client IP: %s", requestID, c.ClientIP())
	log.Printf("🔗 [%s] Headers: %+v", requestID, c.Request.Header)

	// Get the authorization header
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

	// Verify the Firebase token
	log.Printf("🔥 [%s] Verifying Firebase token...", requestID)
	userID, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		log.Printf("❌ [%s] Firebase token verification failed: %v", requestID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication token", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Firebase token verified for user: %s", requestID, userID)

	// Get user subscription
	log.Printf("🗄️ [%s] Fetching subscription URLs for user %s", requestID, userID)
	ctx := context.Background()
	subscription, err := dynamo.GetSubscription(ctx, userID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get subscription from database: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription", "details": err.Error()})
		return
	}

	if subscription == nil {
		log.Printf("📋 [%s] No subscription found for user %s", requestID, userID)
		duration := time.Since(startTime)
		c.JSON(http.StatusOK, gin.H{
			"urls":            nil,
			"message":         "No active subscription found",
			"request_id":      requestID,
			"processing_time": duration.String(),
		})
		log.Printf("✅ [%s] No subscription URLs response sent in %v", requestID, duration)
		return
	}

	log.Printf("🔗 [%s] Subscription URLs for user %s:", requestID, userID)
	log.Printf("   CustomerPortalURL: %s", subscription.CustomerPortalURL)
	log.Printf("   UpdatePaymentMethodURL: %s", subscription.UpdatePaymentMethodURL)
	log.Printf("   CustomerPortalUpdateSubscriptionURL: %s", subscription.CustomerPortalUpdateSubscriptionURL)

	duration := time.Since(startTime)
	response := gin.H{
		"urls": gin.H{
			"customer_portal":                     subscription.CustomerPortalURL,
			"update_payment_method":               subscription.UpdatePaymentMethodURL,
			"customer_portal_update_subscription": subscription.CustomerPortalUpdateSubscriptionURL,
		},
		"request_id":      requestID,
		"processing_time": duration.String(),
	}

	c.JSON(http.StatusOK, response)
	log.Printf("✅ [%s] Subscription URLs response sent in %v", requestID, duration)
}

// CancelSubscriptionHandler handles POST /api/cancel-subscription
func CancelSubscriptionHandler(c *gin.Context) {
	startTime := time.Now()
	requestID := fmt.Sprintf("cancel-%d", startTime.UnixNano())

	log.Printf("❌ [%s] Cancel subscription request started", requestID)
	log.Printf("❌ [%s] Client IP: %s", requestID, c.ClientIP())
	log.Printf("❌ [%s] Headers: %+v", requestID, c.Request.Header)

	// Get the authorization header
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

	// Verify the Firebase token
	log.Printf("🔥 [%s] Verifying Firebase token...", requestID)
	userID, err := firebase.VerifyIDToken(context.Background(), idToken)
	if err != nil {
		log.Printf("❌ [%s] Firebase token verification failed: %v", requestID, err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authentication token", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Firebase token verified for user: %s", requestID, userID)

	// Get user subscription
	log.Printf("🗄️ [%s] Fetching subscription for cancellation: %s", requestID, userID)
	ctx := context.Background()
	subscription, err := dynamo.GetSubscription(ctx, userID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get subscription from database: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get subscription", "details": err.Error()})
		return
	}

	if subscription == nil {
		log.Printf("❌ [%s] No subscription found for user %s", requestID, userID)
		duration := time.Since(startTime)
		c.JSON(http.StatusNotFound, gin.H{
			"error":           "No active subscription found",
			"request_id":      requestID,
			"processing_time": duration.String(),
		})
		log.Printf("❌ [%s] No subscription found response sent in %v", requestID, duration)
		return
	}

	log.Printf("❌ [%s] Found subscription to cancel:", requestID)
	log.Printf("   Tier: %s", subscription.Tier)
	log.Printf("   Status: %s", subscription.Status)
	log.Printf("   SubID: %s", subscription.SubID)

	// Check if already cancelled
	if subscription.Status == "cancelled" {
		log.Printf("⚠️ [%s] Subscription already cancelled for user %s", requestID, userID)
		duration := time.Since(startTime)
		c.JSON(http.StatusOK, gin.H{
			"message":         "Subscription is already cancelled",
			"status":          subscription.Status,
			"request_id":      requestID,
			"processing_time": duration.String(),
		})
		log.Printf("⚠️ [%s] Already cancelled response sent in %v", requestID, duration)
		return
	}

	// Update subscription status to cancelled
	log.Printf("❌ [%s] Marking subscription as cancelled in database", requestID)
	subscription.Status = "cancelled"
	subscription.UpdatedAt = time.Now()

	err = dynamo.SaveSubscriptionDetailed(ctx, *subscription)
	if err != nil {
		log.Printf("❌ [%s] Failed to update subscription status: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to cancel subscription", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Subscription cancelled successfully for user %s", requestID, userID)

	duration := time.Since(startTime)
	response := gin.H{
		"message":         "Subscription cancelled successfully",
		"status":          subscription.Status,
		"cancelled_at":    subscription.UpdatedAt.Format(time.RFC3339),
		"request_id":      requestID,
		"processing_time": duration.String(),
	}

	c.JSON(http.StatusOK, response)
	log.Printf("✅ [%s] Cancellation response sent in %v", requestID, duration)
}

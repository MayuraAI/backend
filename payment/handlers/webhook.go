package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"payment/dynamo"
	"payment/lsz"

	"github.com/gin-gonic/gin"
)

// WebhookHandler handles POST /api/webhook
func WebhookHandler(c *gin.Context) {
	startTime := time.Now()
	requestID := fmt.Sprintf("webhook-%d", startTime.UnixNano())

	log.Printf("🪝 [%s] Webhook request started", requestID)
	log.Printf("🪝 [%s] Method: %s, URL: %s", requestID, c.Request.Method, c.Request.URL.String())
	log.Printf("🪝 [%s] Headers: %+v", requestID, c.Request.Header)
	log.Printf("🪝 [%s] Remote Address: %s", requestID, c.ClientIP())

	// Read the request body
	log.Printf("🪝 [%s] Reading request body...", requestID)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		log.Printf("❌ [%s] Failed to read request body: %v", requestID, err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	bodySize := len(body)
	log.Printf("🪝 [%s] Request body read successfully (%d bytes)", requestID, bodySize)
	if bodySize > 0 {
		// Log first 200 characters of body for debugging (be careful with sensitive data)
		preview := string(body)
		if len(preview) > 200 {
			preview = preview[:200] + "..."
		}
		log.Printf("🪝 [%s] Body preview: %s", requestID, preview)
	}

	// Get the signature from headers
	signature := c.GetHeader("X-Signature")
	log.Printf("🪝 [%s] X-Signature header: %s", requestID,
		func() string {
			if signature == "" {
				return "❌ Not provided"
			}
			return "✅ Present"
		}())

	// Verify webhook signature (if configured)
	log.Printf("🪝 [%s] Verifying webhook signature...", requestID)
	if !lsz.VerifyWebhookSignature(body, signature) {
		log.Printf("❌ [%s] Invalid webhook signature verification failed", requestID)
		log.Printf("❌ [%s] Request rejected due to signature mismatch", requestID)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid webhook signature"})
		return
	}
	log.Printf("✅ [%s] Webhook signature verified successfully", requestID)

	// Parse the webhook payload
	log.Printf("🪝 [%s] Parsing webhook payload...", requestID)
	var payload lsz.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Printf("❌ [%s] Failed to parse webhook payload: %v", requestID, err)
		log.Printf("❌ [%s] Invalid JSON in request body", requestID)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook payload", "details": err.Error()})
		return
	}

	log.Printf("✅ [%s] Webhook payload parsed successfully", requestID)
	log.Printf("🪝 [%s] Event: %s", requestID, payload.Meta.EventName)
	log.Printf("🪝 [%s] Subscription ID: %s", requestID, payload.Data.ID)
	log.Printf("🪝 [%s] Customer ID: %d", requestID, payload.Data.Attributes.CustomerID)
	log.Printf("🪝 [%s] User Email: %s", requestID, payload.Data.Attributes.UserEmail)
	log.Printf("🪝 [%s] Status: %s", requestID, payload.Data.Attributes.Status)
	log.Printf("🪝 [%s] Variant ID: %d", requestID, payload.Data.Attributes.VariantID)

	// Process the webhook event
	log.Printf("🪝 [%s] Processing webhook event...", requestID)
	err = processWebhookEvent(payload, requestID)
	if err != nil {
		log.Printf("❌ [%s] Failed to process webhook: %v", requestID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process webhook", "details": err.Error()})
		return
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Webhook processed successfully in %v", requestID, duration)
	c.JSON(http.StatusOK, gin.H{
		"message":         "Webhook processed successfully",
		"request_id":      requestID,
		"processing_time": duration.String(),
	})
}

// processWebhookEvent processes different types of webhook events
func processWebhookEvent(payload lsz.WebhookPayload, requestID string) error {
	log.Printf("🔄 [%s] Processing event: %s", requestID, payload.Meta.EventName)

	// Extract user ID from custom data
	log.Printf("🔍 [%s] Extracting user ID from custom data...", requestID)
	userID := ""
	if customData, ok := payload.Data.Attributes.CustomData["user_id"]; ok {
		if uid, ok := customData.(string); ok {
			userID = uid
			log.Printf("✅ [%s] User ID found in attributes custom data: %s", requestID, userID)
		}
	}

	// If no user ID found, try to get from custom field
	if userID == "" && payload.Meta.CustomData != nil {
		log.Printf("🔍 [%s] Trying to get user ID from meta custom data...", requestID)
		if customUserID, ok := payload.Meta.CustomData["user_id"]; ok {
			if uid, ok := customUserID.(string); ok {
				userID = uid
				log.Printf("✅ [%s] User ID found in meta custom data: %s", requestID, userID)
			}
		}
	}

	if userID == "" {
		log.Printf("❌ [%s] No user_id found in webhook payload", requestID)
		log.Printf("❌ [%s] Attributes custom data: %+v", requestID, payload.Data.Attributes.CustomData)
		log.Printf("❌ [%s] Meta custom data: %+v", requestID, payload.Meta.CustomData)
		return fmt.Errorf("no user_id found in webhook payload")
	}

	// Get the tier from variant ID
	log.Printf("🔍 [%s] Getting tier for variant ID: %d", requestID, payload.Data.Attributes.VariantID)
	tier := lsz.GetVariantTier(payload.Data.Attributes.VariantID)
	log.Printf("🎫 [%s] Variant %d mapped to tier: %s", requestID, payload.Data.Attributes.VariantID, tier)

	if tier == "free" {
		log.Printf("❌ [%s] Invalid variant ID: %d (mapped to free tier)", requestID, payload.Data.Attributes.VariantID)
		return fmt.Errorf("invalid variant ID: %d", payload.Data.Attributes.VariantID)
	}

	// Create subscription object
	log.Printf("🏗️ [%s] Creating subscription object for user %s", requestID, userID)
	subscription := dynamo.Subscription{
		UserID:                              userID,
		Tier:                                tier,
		VariantID:                           payload.Data.Attributes.VariantID,
		Status:                              payload.Data.Attributes.Status,
		SubID:                               payload.Data.ID,
		UpdatedAt:                           time.Now(),
		CreatedAt:                           time.Now(),
		CustomerID:                          fmt.Sprintf("%d", payload.Data.Attributes.CustomerID),
		Email:                               payload.Data.Attributes.UserEmail,
		CustomerPortalURL:                   payload.Data.Attributes.URLs.CustomerPortal,
		UpdatePaymentMethodURL:              payload.Data.Attributes.URLs.UpdatePaymentMethod,
		CustomerPortalUpdateSubscriptionURL: payload.Data.Attributes.URLs.CustomerPortalUpdateSubscription,
	}

	log.Printf("🏗️ [%s] Subscription object created:", requestID)
	log.Printf("   UserID: %s", userID)
	log.Printf("   Tier: %s", tier)
	log.Printf("   Status: %s", subscription.Status)
	log.Printf("   CustomerID: %s", subscription.CustomerID)
	log.Printf("   Email: %s", subscription.Email)
	log.Printf("   CustomerPortalURL: %s", subscription.CustomerPortalURL)
	log.Printf("   UpdatePaymentMethodURL: %s", subscription.UpdatePaymentMethodURL)
	log.Printf("   CustomerPortalUpdateSubscriptionURL: %s", subscription.CustomerPortalUpdateSubscriptionURL)

	// Parse dates if available
	if payload.Data.Attributes.EndsAt != nil {
		log.Printf("📅 [%s] Parsing ends_at date: %s", requestID, *payload.Data.Attributes.EndsAt)
		if endsAt, err := time.Parse(time.RFC3339, *payload.Data.Attributes.EndsAt); err == nil {
			subscription.ExpiresAt = &endsAt
			log.Printf("✅ [%s] Expires at: %s", requestID, endsAt.Format(time.RFC3339))
		} else {
			log.Printf("⚠️ [%s] Failed to parse ends_at date: %v", requestID, err)
		}
	} else {
		log.Printf("📅 [%s] No ends_at date provided", requestID)
	}

	// Process different event types
	log.Printf("🔀 [%s] Routing to event handler for: %s", requestID, payload.Meta.EventName)
	switch payload.Meta.EventName {
	case "subscription_created":
		log.Printf("🆕 [%s] Handling subscription_created event", requestID)
		return handleSubscriptionCreated(subscription, requestID)
	case "subscription_updated":
		log.Printf("🔄 [%s] Handling subscription_updated event", requestID)
		return handleSubscriptionUpdated(subscription, requestID)
	case "subscription_cancelled":
		log.Printf("❌ [%s] Handling subscription_cancelled event", requestID)
		return handleSubscriptionCancelled(subscription, requestID)
	case "subscription_plan_changed":
		log.Printf("🔄 [%s] Handling subscription_plan_changed event", requestID)
		return handleSubscriptionPlanChanged(subscription, requestID)
	case "subscription_resumed":
		log.Printf("▶️ [%s] Handling subscription_resumed event", requestID)
		return handleSubscriptionResumed(subscription, requestID)
	case "subscription_expired":
		log.Printf("⏰ [%s] Handling subscription_expired event", requestID)
		return handleSubscriptionExpired(subscription, requestID)
	case "subscription_paused":
		log.Printf("⏸️ [%s] Handling subscription_paused event", requestID)
		return handleSubscriptionPaused(subscription, requestID)
	case "subscription_unpaused":
		log.Printf("▶️ [%s] Handling subscription_unpaused event", requestID)
		return handleSubscriptionUnpaused(subscription, requestID)
	default:
		// Log unknown event type but don't fail
		log.Printf("⚠️ [%s] Unknown webhook event type: %s", requestID, payload.Meta.EventName)
		log.Printf("⚠️ [%s] Event will be ignored (not an error)", requestID)
		return nil
	}
}

// handleSubscriptionCreated handles new subscription creation
func handleSubscriptionCreated(sub dynamo.Subscription, requestID string) error {
	log.Printf("🆕 [%s] Creating new subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Set created time for new subscription
	sub.CreatedAt = time.Now()
	sub.UpdatedAt = time.Now()

	log.Printf("🆕 [%s] Saving new subscription to database...", requestID)
	err := dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save new subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] New subscription created successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionUpdated handles subscription updates
func handleSubscriptionUpdated(sub dynamo.Subscription, requestID string) error {
	log.Printf("🔄 [%s] Updating subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.UpdatedAt = time.Now()

	log.Printf("🔄 [%s] Saving updated subscription to database...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save updated subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription updated successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionCancelled handles subscription cancellation
func handleSubscriptionCancelled(sub dynamo.Subscription, requestID string) error {
	log.Printf("❌ [%s] Cancelling subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.Status = "cancelled"
	sub.UpdatedAt = time.Now()

	log.Printf("❌ [%s] Setting status to cancelled and saving...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save cancelled subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription cancelled successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionResumed handles subscription resumption
func handleSubscriptionResumed(sub dynamo.Subscription, requestID string) error {
	log.Printf("▶️ [%s] Resuming subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.Status = "active"
	sub.UpdatedAt = time.Now()

	log.Printf("▶️ [%s] Setting status to active and saving...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save resumed subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription resumed successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionExpired handles subscription expiration
func handleSubscriptionExpired(sub dynamo.Subscription, requestID string) error {
	log.Printf("⏰ [%s] Expiring subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.Status = "expired"
	sub.UpdatedAt = time.Now()

	log.Printf("⏰ [%s] Setting status to expired and saving...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save expired subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription expired successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionPaused handles subscription pausing
func handleSubscriptionPaused(sub dynamo.Subscription, requestID string) error {
	log.Printf("⏸️ [%s] Pausing subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.Status = "paused"
	sub.UpdatedAt = time.Now()

	log.Printf("⏸️ [%s] Setting status to paused and saving...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save paused subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription paused successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionUnpaused handles subscription unpausing
func handleSubscriptionUnpaused(sub dynamo.Subscription, requestID string) error {
	log.Printf("▶️ [%s] Unpausing subscription for user %s", requestID, sub.UserID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	sub.Status = "active"
	sub.UpdatedAt = time.Now()

	log.Printf("▶️ [%s] Setting status to active and saving...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save unpaused subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription unpaused successfully for user %s", requestID, sub.UserID)
	return nil
}

// handleSubscriptionPlanChanged handles subscription plan changes
func handleSubscriptionPlanChanged(sub dynamo.Subscription, requestID string) error {
	log.Printf("🔄 [%s] Plan changed for subscription user %s", requestID, sub.UserID)
	log.Printf("🔄 [%s] New tier: %s, Variant ID: %d", requestID, sub.Tier, sub.VariantID)
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	log.Printf("🔍 [%s] Fetching existing subscription data...", requestID)
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		log.Printf("❌ [%s] Failed to get existing subscription: %v", requestID, err)
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
		log.Printf("✅ [%s] Preserved original creation date: %s", requestID, existing.CreatedAt.Format(time.RFC3339))
		log.Printf("🔄 [%s] Plan change: %s -> %s", requestID, existing.Tier, sub.Tier)
	} else {
		sub.CreatedAt = time.Now()
		log.Printf("⚠️ [%s] No existing subscription found, using current time as creation date", requestID)
	}

	// Keep the current status (usually "active" when plan changes)
	sub.UpdatedAt = time.Now()

	log.Printf("🔄 [%s] Saving plan change to database...", requestID)
	err = dynamo.SaveSubscriptionDetailed(ctx, sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to save plan changed subscription: %v", requestID, err)
		return err
	}

	log.Printf("✅ [%s] Subscription plan changed successfully for user %s", requestID, sub.UserID)
	return nil
}

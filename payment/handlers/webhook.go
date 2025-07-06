package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"payment/dynamo"
	"payment/lsz"

	"github.com/gin-gonic/gin"
)

// WebhookHandler handles POST /api/webhook
func WebhookHandler(c *gin.Context) {
	// Read the request body
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read request body"})
		return
	}

	// Get the signature from headers
	signature := c.GetHeader("X-Signature")

	// Verify webhook signature (if configured)
	if !lsz.VerifyWebhookSignature(body, signature) {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid webhook signature"})
		return
	}

	// Parse the webhook payload
	var payload lsz.WebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid webhook payload", "details": err.Error()})
		return
	}

	// Process the webhook event
	err = processWebhookEvent(payload)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to process webhook", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Webhook processed successfully"})
}

// processWebhookEvent processes different types of webhook events
func processWebhookEvent(payload lsz.WebhookPayload) error {
	// Extract user ID from custom data
	userID := ""
	if customData, ok := payload.Data.Attributes.CustomData["user_id"]; ok {
		if uid, ok := customData.(string); ok {
			userID = uid
		}
	}

	// If no user ID found, try to get from custom field
	if userID == "" && payload.Meta.CustomData != nil {
		if customUserID, ok := payload.Meta.CustomData["user_id"]; ok {
			if uid, ok := customUserID.(string); ok {
				userID = uid
			}
		}
	}

	if userID == "" {
		return fmt.Errorf("no user_id found in webhook payload")
	}

	// Get the tier from variant ID
	tier := lsz.GetVariantTier(payload.Data.Attributes.VariantID)
	if tier == "free" {
		return fmt.Errorf("invalid variant ID: %d", payload.Data.Attributes.VariantID)
	}

	// Create subscription object
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

	// Parse dates if available
	if payload.Data.Attributes.EndsAt != nil {
		if endsAt, err := time.Parse(time.RFC3339, *payload.Data.Attributes.EndsAt); err == nil {
			subscription.ExpiresAt = &endsAt
		}
	}

	// Process different event types
	switch payload.Meta.EventName {
	case "subscription_created":
		return handleSubscriptionCreated(subscription)
	case "subscription_updated":
		return handleSubscriptionUpdated(subscription)
	case "subscription_cancelled":
		return handleSubscriptionCancelled(subscription)
	case "subscription_plan_changed":
		return handleSubscriptionPlanChanged(subscription)
	case "subscription_resumed":
		return handleSubscriptionResumed(subscription)
	case "subscription_expired":
		return handleSubscriptionExpired(subscription)
	case "subscription_paused":
		return handleSubscriptionPaused(subscription)
	case "subscription_unpaused":
		return handleSubscriptionUnpaused(subscription)
	default:
		// Log unknown event type but don't fail
		fmt.Printf("Unknown webhook event type: %s\n", payload.Meta.EventName)
		return nil
	}
}

// handleSubscriptionCreated handles new subscription creation
func handleSubscriptionCreated(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Set created time for new subscription
	sub.CreatedAt = time.Now()
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionUpdated handles subscription updates
func handleSubscriptionUpdated(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionCancelled handles subscription cancellation
func handleSubscriptionCancelled(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.Status = "cancelled"
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionResumed handles subscription resumption
func handleSubscriptionResumed(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.Status = "active"
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionExpired handles subscription expiration
func handleSubscriptionExpired(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.Status = "expired"
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionPaused handles subscription pausing
func handleSubscriptionPaused(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.Status = "paused"
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionUnpaused handles subscription unpausing
func handleSubscriptionUnpaused(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	sub.Status = "active"
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

// handleSubscriptionPlanChanged handles subscription plan changes
func handleSubscriptionPlanChanged(sub dynamo.Subscription) error {
	ctx := context.Background()

	// Get existing subscription to preserve created_at
	existing, err := dynamo.GetSubscription(ctx, sub.UserID)
	if err != nil {
		return err
	}

	if existing != nil {
		sub.CreatedAt = existing.CreatedAt
	} else {
		sub.CreatedAt = time.Now()
	}

	// Keep the current status (usually "active" when plan changes)
	sub.UpdatedAt = time.Now()

	return dynamo.SaveSubscriptionDetailed(ctx, sub)
}

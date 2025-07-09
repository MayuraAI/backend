package lsz

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// LemonSqueezy API configuration
const (
	BaseURL = "https://api.lemonsqueezy.com/v1"

	// Variant IDs for subscription tiers
	PlusVariantID = 890080 // Plus tier
	ProVariantID  = 890081 // Pro tier
)

// GetTierVariantID returns the variant ID for a given tier
func GetTierVariantID(tier string) int {
	requestID := fmt.Sprintf("variant-%d", time.Now().UnixNano())
	log.Printf("🍋 [%s] Getting variant ID for tier: %s", requestID, tier)

	switch strings.ToLower(tier) {
	case "plus":
		log.Printf("✅ [%s] Mapped tier '%s' to variant ID: %d", requestID, tier, PlusVariantID)
		return PlusVariantID
	case "pro":
		log.Printf("✅ [%s] Mapped tier '%s' to variant ID: %d", requestID, tier, ProVariantID)
		return ProVariantID
	default:
		log.Printf("❌ [%s] Unknown tier: %s", requestID, tier)
		return 0
	}
}

// GetVariantTier returns the tier for a given variant ID
func GetVariantTier(variantID int) string {
	requestID := fmt.Sprintf("tier-%d", time.Now().UnixNano())
	log.Printf("🍋 [%s] Getting tier for variant ID: %d", requestID, variantID)

	switch variantID {
	case PlusVariantID:
		log.Printf("✅ [%s] Mapped variant ID %d to tier: plus", requestID, variantID)
		return "plus"
	case ProVariantID:
		log.Printf("✅ [%s] Mapped variant ID %d to tier: pro", requestID, variantID)
		return "pro"
	default:
		log.Printf("❌ [%s] Unknown variant ID: %d, defaulting to free", requestID, variantID)
		return "free"
	}
}

// IsValidSubscriptionStatus checks if a subscription status is considered active
func IsValidSubscriptionStatus(status string) bool {
	requestID := fmt.Sprintf("status-%d", time.Now().UnixNano())
	log.Printf("🔍 [%s] Checking if status is valid: %s", requestID, status)

	validStatuses := []string{"active", "trialing", "past_due"}

	for _, validStatus := range validStatuses {
		if status == validStatus {
			log.Printf("✅ [%s] Status '%s' is valid", requestID, status)
			return true
		}
	}

	log.Printf("❌ [%s] Status '%s' is not valid (not in: %v)", requestID, status, validStatuses)
	return false
}

// CheckoutRequest represents the request payload for creating a checkout
type CheckoutRequest struct {
	Data CheckoutData `json:"data"`
}

type CheckoutData struct {
	Type          string                `json:"type"`
	Attributes    CheckoutAttributes    `json:"attributes"`
	Relationships CheckoutRelationships `json:"relationships"`
}

type CheckoutAttributes struct {
	CustomPrice     *int                   `json:"custom_price,omitempty"`
	ProductOptions  CheckoutProductOptions `json:"product_options,omitempty"`
	CheckoutOptions CheckoutOptions        `json:"checkout_options,omitempty"`
	CheckoutData    CheckoutCustomData     `json:"checkout_data,omitempty"`
	Preview         bool                   `json:"preview,omitempty"`
	TestMode        bool                   `json:"test_mode,omitempty"`
	ExpiresAt       *string                `json:"expires_at,omitempty"`
}

type CheckoutProductOptions struct {
	Name                string   `json:"name,omitempty"`
	Description         string   `json:"description,omitempty"`
	Media               []string `json:"media,omitempty"`
	RedirectURL         string   `json:"redirect_url,omitempty"`
	ReceiptButtonText   string   `json:"receipt_button_text,omitempty"`
	ReceiptLinkURL      string   `json:"receipt_link_url,omitempty"`
	ReceiptThankYouNote string   `json:"receipt_thank_you_note,omitempty"`
	EnabledVariants     []int    `json:"enabled_variants,omitempty"`
}

type CheckoutOptions struct {
	Embed               bool   `json:"embed,omitempty"`
	Media               *bool  `json:"media,omitempty"`
	Logo                *bool  `json:"logo,omitempty"`
	Desc                *bool  `json:"desc,omitempty"`
	Discount            *bool  `json:"discount,omitempty"`
	SkipTrial           bool   `json:"skip_trial,omitempty"`
	SubscriptionPreview *bool  `json:"subscription_preview,omitempty"`
	ButtonColor         string `json:"button_color,omitempty"`
}

type CheckoutCustomData struct {
	Email             string                 `json:"email,omitempty"`
	Name              string                 `json:"name,omitempty"`
	BillingAddress    map[string]interface{} `json:"billing_address,omitempty"`
	TaxNumber         string                 `json:"tax_number,omitempty"`
	DiscountCode      string                 `json:"discount_code,omitempty"`
	Custom            map[string]interface{} `json:"custom,omitempty"`
	VariantQuantities []VariantQuantity      `json:"variant_quantities,omitempty"`
}

type VariantQuantity struct {
	VariantID int `json:"variant_id"`
	Quantity  int `json:"quantity"`
}

type CheckoutRelationships struct {
	Store   RelationshipData `json:"store"`
	Variant RelationshipData `json:"variant"`
}

type RelationshipData struct {
	Data RelationshipItem `json:"data"`
}

type RelationshipItem struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// CheckoutResponse represents the response from creating a checkout
type CheckoutResponse struct {
	Data CheckoutResponseData `json:"data"`
}

type CheckoutResponseData struct {
	Type       string                     `json:"type"`
	ID         string                     `json:"id"`
	Attributes CheckoutResponseAttributes `json:"attributes"`
	Meta       CheckoutResponseMeta       `json:"meta"`
}

type CheckoutResponseAttributes struct {
	StoreID   int    `json:"store_id"`
	VariantID int    `json:"variant_id"`
	URL       string `json:"url"`
	TestMode  bool   `json:"test_mode"`
	ExpiresAt string `json:"expires_at"`
}

type CheckoutResponseMeta struct {
	TestMode bool `json:"test_mode"`
}

// WebhookPayload represents the LemonSqueezy webhook payload
type WebhookPayload struct {
	Meta WebhookMeta `json:"meta"`
	Data WebhookData `json:"data"`
}

type WebhookMeta struct {
	EventName  string                 `json:"event_name"`
	CustomData map[string]interface{} `json:"custom_data"`
}

type WebhookData struct {
	Type       string                `json:"type"`
	ID         string                `json:"id"`
	Attributes WebhookDataAttributes `json:"attributes"`
}

type WebhookDataAttributes struct {
	StoreID    int                    `json:"store_id"`
	CustomerID int                    `json:"customer_id"`
	VariantID  int                    `json:"variant_id"`
	UserEmail  string                 `json:"user_email"`
	Status     string                 `json:"status"`
	EndsAt     *string                `json:"ends_at"`
	CreatedAt  string                 `json:"created_at"`
	UpdatedAt  string                 `json:"updated_at"`
	CustomData map[string]interface{} `json:"custom_data"`
	URLs       WebhookURLs            `json:"urls"`
}

type WebhookURLs struct {
	CustomerPortal                   string `json:"customer_portal"`
	UpdatePaymentMethod              string `json:"update_payment_method"`
	CustomerPortalUpdateSubscription string `json:"customer_portal_update_subscription"`
}

// CreateCheckout creates a checkout session with LemonSqueezy
func CreateCheckout(variantID int, userID, email string) (*CheckoutResponse, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("checkout-%d", startTime.UnixNano())

	log.Printf("🍋 [%s] Creating LemonSqueezy checkout session", requestID)
	log.Printf("🍋 [%s] Variant ID: %d", requestID, variantID)
	log.Printf("🍋 [%s] User ID: %s", requestID, userID)
	log.Printf("🍋 [%s] Email: %s", requestID, email)

	// Get API key
	apiKey := os.Getenv("LSZ_API_KEY")
	if apiKey == "" {
		log.Printf("❌ [%s] LSZ_API_KEY environment variable not set", requestID)
		return nil, fmt.Errorf("LSZ_API_KEY environment variable not set")
	}
	log.Printf("🔐 [%s] API key loaded successfully", requestID)

	// Get store ID
	storeID := os.Getenv("LSZ_STORE_ID")
	if storeID == "" {
		log.Printf("❌ [%s] LSZ_STORE_ID environment variable not set", requestID)
		return nil, fmt.Errorf("LSZ_STORE_ID environment variable not set")
	}
	log.Printf("🏪 [%s] Store ID: %s", requestID, storeID)

	// Get environment configuration
	redirectURL := os.Getenv("LSZ_REDIRECT_URL")
	if redirectURL == "" {
		redirectURL = "https://mayura.rocks/dashboard"
		log.Printf("⚠️ [%s] LSZ_REDIRECT_URL not set, using default: %s", requestID, redirectURL)
	} else {
		log.Printf("🔗 [%s] Redirect URL: %s", requestID, redirectURL)
	}

	receiptURL := os.Getenv("LSZ_RECEIPT_URL")
	if receiptURL == "" {
		receiptURL = "https://mayura.rocks/receipt"
		log.Printf("⚠️ [%s] LSZ_RECEIPT_URL not set, using default: %s", requestID, receiptURL)
	} else {
		log.Printf("🧾 [%s] Receipt URL: %s", requestID, receiptURL)
	}

	// Check if we're in test mode
	testMode := os.Getenv("LSZ_TEST_MODE") == "true" || os.Getenv("DEVELOPMENT") == "true"
	log.Printf("🧪 [%s] Test mode: %v", requestID, testMode)

	// Create the checkout request payload matching LemonSqueezy API exactly
	log.Printf("🏗️ [%s] Building checkout request payload...", requestID)
	checkout := CheckoutRequest{
		Data: CheckoutData{
			Type: "checkouts",
			Attributes: CheckoutAttributes{
				ProductOptions: CheckoutProductOptions{
					EnabledVariants:     []int{variantID},
					RedirectURL:         redirectURL,
					ReceiptLinkURL:      receiptURL,
					ReceiptThankYouNote: "Thank you for subscribing to Mayura AI!",
				},
				CheckoutOptions: CheckoutOptions{
					Embed:               false,
					Media:               boolPtr(true),
					Logo:                boolPtr(true),
					Desc:                boolPtr(true),
					Discount:            boolPtr(true),
					SkipTrial:           false,
					SubscriptionPreview: boolPtr(true),
					ButtonColor:         "#7047EB",
				},
				CheckoutData: CheckoutCustomData{
					Email: email,
					Custom: map[string]interface{}{
						"user_id": userID,
					},
				},
				Preview:  false,
				TestMode: testMode,
			},
			Relationships: CheckoutRelationships{
				Store: RelationshipData{
					Data: RelationshipItem{
						Type: "stores",
						ID:   storeID,
					},
				},
				Variant: RelationshipData{
					Data: RelationshipItem{
						Type: "variants",
						ID:   fmt.Sprintf("%d", variantID),
					},
				},
			},
		},
	}

	// Marshal the request
	log.Printf("🔄 [%s] Marshaling checkout request...", requestID)
	reqBody, err := json.Marshal(checkout)
	if err != nil {
		log.Printf("❌ [%s] Failed to marshal checkout request: %v", requestID, err)
		return nil, fmt.Errorf("failed to marshal checkout request: %w", err)
	}

	log.Printf("✅ [%s] Request payload created (%d bytes)", requestID, len(reqBody))
	// Log first 500 characters of request for debugging
	reqPreview := string(reqBody)
	if len(reqPreview) > 500 {
		reqPreview = reqPreview[:500] + "..."
	}
	log.Printf("🔍 [%s] Request preview: %s", requestID, reqPreview)

	// Create HTTP request
	url := fmt.Sprintf("%s/checkouts", BaseURL)
	log.Printf("🌐 [%s] Creating HTTP POST request to: %s", requestID, url)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		log.Printf("❌ [%s] Failed to create HTTP request: %v", requestID, err)
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.api+json")
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	log.Printf("🔧 [%s] HTTP headers set:", requestID)
	log.Printf("   Accept: application/vnd.api+json")
	log.Printf("   Content-Type: application/vnd.api+json")
	log.Printf("   Authorization: Bearer [REDACTED]")

	// Make the request
	log.Printf("📤 [%s] Sending HTTP request to LemonSqueezy...", requestID)
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Printf("❌ [%s] HTTP request failed: %v", requestID, err)
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	log.Printf("📥 [%s] Response received - Status: %d %s", requestID, resp.StatusCode, resp.Status)
	log.Printf("📥 [%s] Response headers: %+v", requestID, resp.Header)

	// Read response body
	log.Printf("📖 [%s] Reading response body...", requestID)
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [%s] Failed to read response body: %v", requestID, err)
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	respBodySize := len(respBody)
	log.Printf("📖 [%s] Response body read (%d bytes)", requestID, respBodySize)

	// Log response preview for debugging
	if respBodySize > 0 {
		respPreview := string(respBody)
		if len(respPreview) > 500 {
			respPreview = respPreview[:500] + "..."
		}
		log.Printf("🔍 [%s] Response preview: %s", requestID, respPreview)
	}

	// Check for errors
	if resp.StatusCode != http.StatusCreated {
		log.Printf("❌ [%s] API returned error status: %d", requestID, resp.StatusCode)
		log.Printf("❌ [%s] Full error response: %s", requestID, string(respBody))
		return nil, fmt.Errorf("API request failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse response
	log.Printf("🔄 [%s] Parsing checkout response...", requestID)
	var checkoutResp CheckoutResponse
	if err := json.Unmarshal(respBody, &checkoutResp); err != nil {
		log.Printf("❌ [%s] Failed to parse response JSON: %v", requestID, err)
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Checkout created successfully in %v", requestID, duration)
	log.Printf("✅ [%s] Checkout details:", requestID)
	log.Printf("   ID: %s", checkoutResp.Data.ID)
	log.Printf("   URL: %s", checkoutResp.Data.Attributes.URL)
	log.Printf("   Store ID: %d", checkoutResp.Data.Attributes.StoreID)
	log.Printf("   Variant ID: %d", checkoutResp.Data.Attributes.VariantID)
	log.Printf("   Test Mode: %v", checkoutResp.Data.Attributes.TestMode)
	log.Printf("   Expires At: %s", checkoutResp.Data.Attributes.ExpiresAt)

	return &checkoutResp, nil
}

// Helper function to create bool pointers
func boolPtr(b bool) *bool {
	return &b
}

// VerifyWebhookSignature verifies the webhook signature from LemonSqueezy
func VerifyWebhookSignature(payload []byte, signature string) bool {
	startTime := time.Now()
	requestID := fmt.Sprintf("verify-%d", startTime.UnixNano())

	log.Printf("🔐 [%s] Verifying webhook signature", requestID)
	log.Printf("🔐 [%s] Payload size: %d bytes", requestID, len(payload))
	log.Printf("🔐 [%s] Signature: %s", requestID,
		func() string {
			if signature == "" {
				return "❌ Empty"
			}
			return fmt.Sprintf("✅ Present (%d chars)", len(signature))
		}())

	// Get webhook secret
	secret := os.Getenv("LSZ_WEBHOOK_SECRET")
	if secret == "" {
		log.Printf("⚠️ [%s] LSZ_WEBHOOK_SECRET not set, skipping signature verification", requestID)
		// In development, we might not have a secret set
		if os.Getenv("DEVELOPMENT") == "true" {
			log.Printf("🔧 [%s] Development mode - allowing request without signature", requestID)
			return true
		}
		log.Printf("❌ [%s] Webhook secret not configured in production", requestID)
		return false
	}

	log.Printf("🔑 [%s] Webhook secret loaded successfully", requestID)

	// If no signature provided, reject
	if signature == "" {
		log.Printf("❌ [%s] No signature provided in request", requestID)
		return false
	}

	// Compute expected signature
	log.Printf("🔄 [%s] Computing expected HMAC signature...", requestID)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	log.Printf("🔍 [%s] Expected signature: %s", requestID, expectedSignature)
	log.Printf("🔍 [%s] Received signature: %s", requestID, signature)

	// Compare signatures
	isValid := hmac.Equal([]byte(expectedSignature), []byte(signature))

	duration := time.Since(startTime)
	if isValid {
		log.Printf("✅ [%s] Webhook signature verification successful in %v", requestID, duration)
	} else {
		log.Printf("❌ [%s] Webhook signature verification failed in %v", requestID, duration)
		log.Printf("❌ [%s] Signatures do not match!", requestID)
	}

	return isValid
}

// GetStoreID returns the store ID from environment
func GetStoreID() string {
	storeID := os.Getenv("LSZ_STORE_ID")
	log.Printf("🏪 Getting store ID from environment: %s",
		func() string {
			if storeID == "" {
				return "❌ Not set"
			}
			return fmt.Sprintf("✅ %s", storeID)
		}())
	return storeID
}

// LogAPIConfiguration logs the current API configuration (safely)
func LogAPIConfiguration() {
	log.Println("🍋 LemonSqueezy API Configuration:")
	log.Printf("  Base URL: %s", BaseURL)
	log.Printf("  Plus Variant ID: %d", PlusVariantID)
	log.Printf("  Pro Variant ID: %d", ProVariantID)

	// Log presence of environment variables without exposing values
	log.Printf("  LSZ_API_KEY: %s",
		func() string {
			if os.Getenv("LSZ_API_KEY") != "" {
				return "✅ Set"
			}
			return "❌ Not set"
		}())

	log.Printf("  LSZ_WEBHOOK_SECRET: %s",
		func() string {
			if os.Getenv("LSZ_WEBHOOK_SECRET") != "" {
				return "✅ Set"
			}
			return "❌ Not set"
		}())

	log.Printf("  LSZ_STORE_ID: %s",
		func() string {
			if os.Getenv("LSZ_STORE_ID") != "" {
				return "✅ Set"
			}
			return "❌ Not set"
		}())

	log.Printf("  LSZ_REDIRECT_URL: %s",
		func() string {
			if url := os.Getenv("LSZ_REDIRECT_URL"); url != "" {
				return fmt.Sprintf("✅ %s", url)
			}
			return "⚠️ Using default"
		}())

	log.Printf("  LSZ_RECEIPT_URL: %s",
		func() string {
			if url := os.Getenv("LSZ_RECEIPT_URL"); url != "" {
				return fmt.Sprintf("✅ %s", url)
			}
			return "⚠️ Using default"
		}())

	log.Printf("  Test Mode: %v", os.Getenv("LSZ_TEST_MODE") == "true" || os.Getenv("DEVELOPMENT") == "true")
}

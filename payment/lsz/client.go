package lsz

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	apiKey        = os.Getenv("LSZ_API_KEY")
	webhookSecret = os.Getenv("LSZ_WEBHOOK_SECRET")
	baseURL       = "https://api.lemonsqueezy.com/v1"
)

// CheckoutResponse represents the response from LemonSqueezy checkout API
type CheckoutResponse struct {
	Data struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			URL             string                 `json:"url"`
			Status          string                 `json:"status"`
			CheckoutOptions map[string]interface{} `json:"checkout_options"`
			CheckoutData    map[string]interface{} `json:"checkout_data"`
			PreviewURL      string                 `json:"preview"`
			CreatedAt       string                 `json:"created_at"`
			UpdatedAt       string                 `json:"updated_at"`
		} `json:"attributes"`
		Relationships struct {
			Store struct {
				Data struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"data"`
			} `json:"store"`
			Variant struct {
				Data struct {
					ID   string `json:"id"`
					Type string `json:"type"`
				} `json:"data"`
			} `json:"variant"`
		} `json:"relationships"`
	} `json:"data"`
}

// WebhookPayload represents the webhook payload structure
type WebhookPayload struct {
	Meta struct {
		EventName  string                 `json:"event_name"`
		CustomData map[string]interface{} `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Attributes struct {
			StoreID         int     `json:"store_id"`
			CustomerID      int     `json:"customer_id"`
			OrderID         int     `json:"order_id"`
			OrderNumber     int     `json:"order_number"`
			ProductID       int     `json:"product_id"`
			VariantID       int     `json:"variant_id"`
			ProductName     string  `json:"product_name"`
			VariantName     string  `json:"variant_name"`
			UserName        string  `json:"user_name"`
			UserEmail       string  `json:"user_email"`
			Status          string  `json:"status"`
			StatusFormatted string  `json:"status_formatted"`
			CardBrand       string  `json:"card_brand"`
			CardLastFour    string  `json:"card_last_four"`
			PausedAt        *string `json:"paused_at"`
			SubscriptionID  int     `json:"subscription_id"`
			CreatedAt       string  `json:"created_at"`
			UpdatedAt       string  `json:"updated_at"`
			TestMode        bool    `json:"test_mode"`
			BillingAnchor   int     `json:"billing_anchor"`
			URLs            struct {
				UpdatePaymentMethod string `json:"update_payment_method"`
				CustomerPortal      string `json:"customer_portal"`
			} `json:"urls"`
			RenewsAt       *string                `json:"renews_at"`
			EndsAt         *string                `json:"ends_at"`
			TrialEndsAt    *string                `json:"trial_ends_at"`
			Price          int                    `json:"price"`
			PriceFormatted string                 `json:"price_formatted"`
			CustomData     map[string]interface{} `json:"custom_data"`
		} `json:"attributes"`
	} `json:"data"`
}

// CreateCheckout creates a new checkout session with LemonSqueezy
func CreateCheckout(variantID int, userID string, userEmail string) (*CheckoutResponse, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("LSZ_API_KEY environment variable not set")
	}

	body := map[string]interface{}{
		"data": map[string]interface{}{
			"type": "checkouts",
			"attributes": map[string]interface{}{
				"checkout_options": map[string]interface{}{
					"embed": false,
					"media": true,
					"logo":  true,
				},
				"checkout_data": map[string]interface{}{
					"email": userEmail,
					"name":  "",
					"billing_address": map[string]interface{}{
						"country": "",
					},
					"tax_number":    "",
					"discount_code": "",
					"custom": map[string]interface{}{
						"user_id": userID,
					},
				},
				"product_options": map[string]interface{}{
					"enabled_variants":       []int{variantID},
					"redirect_url":           os.Getenv("LSZ_REDIRECT_URL"),
					"receipt_link_url":       os.Getenv("LSZ_RECEIPT_URL"),
					"receipt_thank_you_note": "Thank you for your purchase!",
					"receipt_button_text":    "Go to Dashboard",
				},
			},
			"relationships": map[string]interface{}{
				"store": map[string]interface{}{
					"data": map[string]interface{}{
						"type": "stores",
						"id":   os.Getenv("LSZ_STORE_ID"),
					},
				},
				"variant": map[string]interface{}{
					"data": map[string]interface{}{
						"type": "variants",
						"id":   strconv.Itoa(variantID),
					},
				},
			},
		},
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", baseURL+"/checkouts", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/vnd.api+json")
	req.Header.Set("Accept", "application/vnd.api+json")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("checkout creation failed with status %d: %s", resp.StatusCode, string(respBody))
	}

	var checkoutResp CheckoutResponse
	if err := json.Unmarshal(respBody, &checkoutResp); err != nil {
		return nil, fmt.Errorf("failed to parse checkout response: %w", err)
	}

	return &checkoutResp, nil
}

// VerifyWebhookSignature verifies the webhook signature from LemonSqueezy
func VerifyWebhookSignature(payload []byte, signature string) bool {
	if webhookSecret == "" {
		// If no webhook secret is set, skip verification (not recommended for production)
		return true
	}

	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(payload)
	expectedSignature := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

// GetVariantTier maps variant ID to subscription tier
func GetVariantTier(variantID int) string {
	// Map variant IDs to subscription tiers
	// These should match your LemonSqueezy product variant IDs
	switch variantID {
	case 123456: // Replace with your actual Plus variant ID
		return "plus"
	case 123457: // Replace with your actual Pro variant ID
		return "pro"
	default:
		return "free"
	}
}

// GetTierVariantID maps subscription tier to variant ID
func GetTierVariantID(tier string) int {
	// Map subscription tiers to variant IDs
	switch tier {
	case "plus":
		return 123456 // Replace with your actual Plus variant ID
	case "pro":
		return 123457 // Replace with your actual Pro variant ID
	default:
		return 0
	}
}

// IsValidSubscriptionStatus checks if the subscription status is active
func IsValidSubscriptionStatus(status string) bool {
	validStatuses := []string{"active", "on_trial", "past_due"}
	for _, validStatus := range validStatuses {
		if status == validStatus {
			return true
		}
	}
	return false
}

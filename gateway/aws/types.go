package aws

import (
	"time"
)

// Profile represents the profiles table
type Profile struct {
	UserID         string    `json:"user_id" dynamodbav:"user_id"` // Primary key
	CreatedAt      time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" dynamodbav:"updated_at"`
	HasOnboarded   bool      `json:"has_onboarded" dynamodbav:"has_onboarded"`
	ProfileContext string    `json:"profile_context" dynamodbav:"profile_context"`
	DisplayName    string    `json:"display_name" dynamodbav:"display_name"`
	Username       string    `json:"username" dynamodbav:"username"` // GSI key for uniqueness
}

// Chat represents the chats table
type Chat struct {
	ID        string    `json:"id" dynamodbav:"id"`           // Primary key
	UserID    string    `json:"user_id" dynamodbav:"user_id"` // GSI key
	CreatedAt time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt time.Time `json:"updated_at" dynamodbav:"updated_at"`
	Sharing   string    `json:"sharing" dynamodbav:"sharing"` // 'private', 'public', etc.
	Name      string    `json:"name" dynamodbav:"name"`
}

// Message represents the messages table
type Message struct {
	ID             string    `json:"id" dynamodbav:"id"`           // Primary key
	ChatID         string    `json:"chat_id" dynamodbav:"chat_id"` // GSI key
	UserID         string    `json:"user_id" dynamodbav:"user_id"` // GSI key
	CreatedAt      time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" dynamodbav:"updated_at"`
	Content        string    `json:"content" dynamodbav:"content"`
	ModelName      string    `json:"model_name" dynamodbav:"model_name"`
	Role           string    `json:"role" dynamodbav:"role"`
	SequenceNumber int       `json:"sequence_number" dynamodbav:"sequence_number"`
}

// Subscription represents the subscriptions table
type Subscription struct {
	ID        string    `json:"id" dynamodbav:"id"`           // Primary key
	UserID    string    `json:"user_id" dynamodbav:"user_id"` // GSI key
	CreatedAt time.Time `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt time.Time `json:"updated_at" dynamodbav:"updated_at"`

	// Subscription Details
	Tier         string `json:"tier" dynamodbav:"tier"`                   // "free", "plus", "pro"
	Status       string `json:"status" dynamodbav:"status"`               // "active", "cancelled", "expired", "trial"
	BillingCycle string `json:"billing_cycle" dynamodbav:"billing_cycle"` // "monthly", "yearly"

	// Dates
	StartDate    time.Time  `json:"start_date" dynamodbav:"start_date"`
	EndDate      *time.Time `json:"end_date" dynamodbav:"end_date"`             // nil for active subscriptions
	TrialEndDate *time.Time `json:"trial_end_date" dynamodbav:"trial_end_date"` // nil if no trial

	// LemonSqueezy Integration
	LemonSqueezyCustomerID     string `json:"lemonsqueezy_customer_id" dynamodbav:"lemonsqueezy_customer_id"`
	LemonSqueezySubscriptionID string `json:"lemonsqueezy_subscription_id" dynamodbav:"lemonsqueezy_subscription_id"`
	LemonSqueezyOrderID        string `json:"lemonsqueezy_order_id" dynamodbav:"lemonsqueezy_order_id"`
	LemonSqueezyProductID      string `json:"lemonsqueezy_product_id" dynamodbav:"lemonsqueezy_product_id"`
	LemonSqueezyVariantID      string `json:"lemonsqueezy_variant_id" dynamodbav:"lemonsqueezy_variant_id"`

	// Pricing
	PricePerMonth float64 `json:"price_per_month" dynamodbav:"price_per_month"`
	Currency      string  `json:"currency" dynamodbav:"currency"` // "USD", "EUR", etc.

	// Usage Limits (based on tier)
	MonthlyTokenLimit int       `json:"monthly_token_limit" dynamodbav:"monthly_token_limit"`
	MonthlyTokenUsed  int       `json:"monthly_token_used" dynamodbav:"monthly_token_used"`
	ResetDate         time.Time `json:"reset_date" dynamodbav:"reset_date"` // When usage resets

	// Features
	CanSelectModel     bool `json:"can_select_model" dynamodbav:"can_select_model"`
	CanAccessAPI       bool `json:"can_access_api" dynamodbav:"can_access_api"`
	MaxChatsPerDay     int  `json:"max_chats_per_day" dynamodbav:"max_chats_per_day"`
	MaxFilesPerChat    int  `json:"max_files_per_chat" dynamodbav:"max_files_per_chat"`
	HasPrioritySupport bool `json:"has_priority_support" dynamodbav:"has_priority_support"`
}

// Table names constants
const (
	ProfilesTableName      = "profiles"
	ChatsTableName         = "chats"
	MessagesTableName      = "messages"
	SubscriptionsTableName = "subscriptions"
)

// GSI names constants
const (
	ProfilesUserIDGSI      = "user_id-gsi"
	ProfilesUsernameGSI    = "username-gsi"
	ChatsUserIDGSI         = "user_id-gsi"
	MessagesChatIDGSI      = "chat_id-gsi"
	MessagesUserIDGSI      = "user_id-gsi"
	SubscriptionsUserIDGSI = "user_id-gsi"
	SubscriptionsTierGSI   = "tier-gsi"   // For analytics
	SubscriptionsStatusGSI = "status-gsi" // For admin queries
)

// Tier definitions
const (
	TierFree = "free"
	TierPlus = "plus"
	TierPro  = "pro"
)

// Subscription status
const (
	StatusActive    = "active"
	StatusTrial     = "trial"
	StatusCancelled = "cancelled"
	StatusExpired   = "expired"
	StatusPastDue   = "past_due"
	StatusPaused    = "paused"
)

// Billing cycles
const (
	BillingMonthly = "monthly"
	BillingYearly  = "yearly"
)

// Tier configurations with dummy pricing
type TierConfig struct {
	Name               string
	MonthlyTokenLimit  int
	PricePerMonth      float64
	PricePerYear       float64
	CanSelectModel     bool
	CanAccessAPI       bool
	MaxChatsPerDay     int
	MaxFilesPerChat    int
	HasPrioritySupport bool
}

var TierConfigs = map[string]TierConfig{
	TierFree: {
		Name:               "Free",
		MonthlyTokenLimit:  10000,
		PricePerMonth:      0,
		PricePerYear:       0,
		CanSelectModel:     false,
		CanAccessAPI:       false,
		MaxChatsPerDay:     10,
		MaxFilesPerChat:    1,
		HasPrioritySupport: false,
	},
	TierPlus: {
		Name:               "Plus",
		MonthlyTokenLimit:  100000,
		PricePerMonth:      9.99,  // Dummy pricing
		PricePerYear:       99.99, // Dummy pricing
		CanSelectModel:     true,
		CanAccessAPI:       false,
		MaxChatsPerDay:     100,
		MaxFilesPerChat:    5,
		HasPrioritySupport: false,
	},
	TierPro: {
		Name:               "Pro",
		MonthlyTokenLimit:  500000,
		PricePerMonth:      29.99,  // Dummy pricing
		PricePerYear:       299.99, // Dummy pricing
		CanSelectModel:     true,
		CanAccessAPI:       true,
		MaxChatsPerDay:     -1, // unlimited
		MaxFilesPerChat:    20,
		HasPrioritySupport: true,
	},
}

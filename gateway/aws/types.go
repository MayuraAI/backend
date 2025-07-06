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

// Subscription represents the subscriptions table (matches payment service structure)
type Subscription struct {
	UserID                              string     `json:"user_id" dynamodbav:"user_id"`
	Tier                                string     `json:"tier" dynamodbav:"tier"`
	Status                              string     `json:"status" dynamodbav:"status"`
	VariantID                           int        `json:"variant_id" dynamodbav:"variant_id"`
	SubID                               string     `json:"sub_id" dynamodbav:"sub_id"`
	CreatedAt                           time.Time  `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt                           time.Time  `json:"updated_at" dynamodbav:"updated_at"`
	ExpiresAt                           *time.Time `json:"expires_at,omitempty" dynamodbav:"expires_at,omitempty"`
	CustomerID                          string     `json:"customer_id" dynamodbav:"customer_id"`
	Email                               string     `json:"email" dynamodbav:"email"`
	CustomerPortalURL                   string     `json:"customer_portal_url" dynamodbav:"customer_portal_url"`
	UpdatePaymentMethodURL              string     `json:"update_payment_method_url" dynamodbav:"update_payment_method_url"`
	CustomerPortalUpdateSubscriptionURL string     `json:"customer_portal_update_subscription_url" dynamodbav:"customer_portal_update_subscription_url"`
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

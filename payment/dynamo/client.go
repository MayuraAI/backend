package dynamo

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var (
	client    *dynamodb.Client
	TableName string
)

// Subscription represents a user's subscription data
type Subscription struct {
	UserID                              string     `dynamodb:"user_id" json:"user_id"`
	Tier                                string     `dynamodb:"tier" json:"tier"`
	Status                              string     `dynamodb:"status" json:"status"`
	VariantID                           int        `dynamodb:"variant_id" json:"variant_id"`
	SubID                               string     `dynamodb:"sub_id" json:"sub_id"`
	CreatedAt                           time.Time  `dynamodb:"created_at" json:"created_at"`
	UpdatedAt                           time.Time  `dynamodb:"updated_at" json:"updated_at"`
	ExpiresAt                           *time.Time `dynamodb:"expires_at,omitempty" json:"expires_at,omitempty"`
	CustomerID                          string     `dynamodb:"customer_id" json:"customer_id"`
	Email                               string     `dynamodb:"email" json:"email"`
	CustomerPortalURL                   string     `dynamodb:"customer_portal_url" json:"customer_portal_url"`
	UpdatePaymentMethodURL              string     `dynamodb:"update_payment_method_url" json:"update_payment_method_url"`
	CustomerPortalUpdateSubscriptionURL string     `dynamodb:"customer_portal_update_subscription_url" json:"customer_portal_update_subscription_url"`
}

// Init initializes the DynamoDB client
func Init() error {
	startTime := time.Now()
	log.Printf("🗄️ DynamoDB initialization started")

	// Get table name from environment
	TableName = os.Getenv("DYNAMO_TABLE")
	if TableName == "" {
		TableName = "subscriptions"
		log.Printf("⚠️ DYNAMO_TABLE not set, using default: %s", TableName)
	} else {
		log.Printf("✅ Using DynamoDB table: %s", TableName)
	}

	// Get AWS region
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
		log.Printf("⚠️ AWS_REGION not set, using default: %s", region)
	} else {
		log.Printf("✅ Using AWS region: %s", region)
	}

	// Check if we're in development mode
	if os.Getenv("DEVELOPMENT") == "true" {
		log.Printf("🔧 Development mode detected - mocking DynamoDB client")
		client = nil // Set to nil to indicate development mode
		duration := time.Since(startTime)
		log.Printf("✅ DynamoDB initialization completed in development mode in %v", duration)
		return nil
	}

	// Try to create AWS config
	log.Printf("🔐 Loading AWS credentials...")
	cfg, err := config.LoadDefaultConfig(context.TODO(), config.WithRegion(region))
	if err != nil {
		// Try with explicit credentials if environment variables are set
		accessKey := os.Getenv("AWS_ACCESS_KEY_ID")
		secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY")

		if accessKey != "" && secretKey != "" {
			log.Printf("🔑 Using explicit AWS credentials from environment")
			cfg, err = config.LoadDefaultConfig(context.TODO(),
				config.WithRegion(region),
				config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
			)
		}

		if err != nil {
			log.Printf("❌ Failed to load AWS config: %v", err)
			return fmt.Errorf("failed to load AWS config: %w", err)
		}
	}

	log.Printf("✅ AWS config loaded successfully")

	// Create DynamoDB client
	log.Printf("🔌 Creating DynamoDB client...")
	client = dynamodb.NewFromConfig(cfg)

	duration := time.Since(startTime)
	log.Printf("✅ DynamoDB client initialized successfully in %v", duration)
	return nil
}

// GetSubscription retrieves a subscription by user ID
func GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("get-%d", startTime.UnixNano())

	log.Printf("🔍 [%s] Getting subscription for user: %s", requestID, userID)
	log.Printf("🔍 [%s] Table: %s", requestID, TableName)

	// Handle development mode
	if client == nil {
		log.Printf("🔧 [%s] Development mode - returning mock subscription", requestID)
		return &Subscription{
			UserID:     userID,
			Tier:       "plus",
			Status:     "active",
			VariantID:  887309,
			SubID:      "dev-sub-123",
			CreatedAt:  time.Now().Add(-24 * time.Hour),
			UpdatedAt:  time.Now(),
			CustomerID: "dev-customer-123",
			Email:      "dev@example.com",
		}, nil
	}

	// Prepare the query
	input := &dynamodb.GetItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	}

	log.Printf("🔍 [%s] Executing DynamoDB GetItem operation...", requestID)
	result, err := client.GetItem(ctx, input)
	if err != nil {
		log.Printf("❌ [%s] DynamoDB GetItem failed: %v", requestID, err)
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	// Check if item exists
	if result.Item == nil {
		log.Printf("📋 [%s] No subscription found for user: %s", requestID, userID)
		duration := time.Since(startTime)
		log.Printf("✅ [%s] GetSubscription completed (no result) in %v", requestID, duration)
		return nil, nil
	}

	// Unmarshal the result
	log.Printf("🔄 [%s] Unmarshaling subscription data...", requestID)
	var subscription Subscription
	err = attributevalue.UnmarshalMap(result.Item, &subscription)
	if err != nil {
		log.Printf("❌ [%s] Failed to unmarshal subscription: %v", requestID, err)
		return nil, fmt.Errorf("failed to unmarshal subscription: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Subscription retrieved successfully in %v", requestID, duration)
	log.Printf("📋 [%s] Subscription details:", requestID)
	log.Printf("   Tier: %s", subscription.Tier)
	log.Printf("   Status: %s", subscription.Status)
	log.Printf("   Variant ID: %d", subscription.VariantID)
	log.Printf("   Created: %s", subscription.CreatedAt.Format(time.RFC3339))
	log.Printf("   Updated: %s", subscription.UpdatedAt.Format(time.RFC3339))
	if subscription.ExpiresAt != nil {
		log.Printf("   Expires: %s", subscription.ExpiresAt.Format(time.RFC3339))
	}

	return &subscription, nil
}

// SaveSubscription saves a subscription to DynamoDB
func SaveSubscription(ctx context.Context, sub Subscription) error {
	startTime := time.Now()
	requestID := fmt.Sprintf("save-%d", startTime.UnixNano())

	log.Printf("💾 [%s] Saving subscription for user: %s", requestID, sub.UserID)
	log.Printf("💾 [%s] Table: %s", requestID, TableName)
	log.Printf("💾 [%s] Subscription data:", requestID)
	log.Printf("   Tier: %s", sub.Tier)
	log.Printf("   Status: %s", sub.Status)
	log.Printf("   Variant ID: %d", sub.VariantID)
	log.Printf("   SubID: %s", sub.SubID)
	log.Printf("   CustomerID: %s", sub.CustomerID)
	log.Printf("   Email: %s", sub.Email)

	// Handle development mode
	if client == nil {
		log.Printf("🔧 [%s] Development mode - simulating save operation", requestID)
		time.Sleep(50 * time.Millisecond) // Simulate database latency
		duration := time.Since(startTime)
		log.Printf("✅ [%s] Subscription saved successfully (development mode) in %v", requestID, duration)
		return nil
	}

	// Ensure timestamps are set
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
		log.Printf("📅 [%s] Set creation time: %s", requestID, sub.CreatedAt.Format(time.RFC3339))
	}
	sub.UpdatedAt = time.Now()
	log.Printf("📅 [%s] Set update time: %s", requestID, sub.UpdatedAt.Format(time.RFC3339))

	// Marshal the subscription
	log.Printf("🔄 [%s] Marshaling subscription data...", requestID)
	item, err := attributevalue.MarshalMap(sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to marshal subscription: %v", requestID, err)
		return fmt.Errorf("failed to marshal subscription: %w", err)
	}

	// Prepare the put item input
	input := &dynamodb.PutItemInput{
		TableName: aws.String(TableName),
		Item:      item,
	}

	log.Printf("💾 [%s] Executing DynamoDB PutItem operation...", requestID)
	_, err = client.PutItem(ctx, input)
	if err != nil {
		log.Printf("❌ [%s] DynamoDB PutItem failed: %v", requestID, err)
		return fmt.Errorf("failed to save subscription: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Subscription saved successfully in %v", requestID, duration)
	return nil
}

// SaveSubscriptionDetailed saves a subscription with detailed logging
func SaveSubscriptionDetailed(ctx context.Context, sub Subscription) error {
	startTime := time.Now()
	requestID := fmt.Sprintf("save-detailed-%d", startTime.UnixNano())

	log.Printf("💾 [%s] Saving subscription with detailed logging for user: %s", requestID, sub.UserID)
	log.Printf("💾 [%s] Table: %s", requestID, TableName)
	log.Printf("💾 [%s] Complete subscription data:", requestID)
	log.Printf("   UserID: %s", sub.UserID)
	log.Printf("   Tier: %s", sub.Tier)
	log.Printf("   Status: %s", sub.Status)
	log.Printf("   VariantID: %d", sub.VariantID)
	log.Printf("   SubID: %s", sub.SubID)
	log.Printf("   CustomerID: %s", sub.CustomerID)
	log.Printf("   Email: %s", sub.Email)
	log.Printf("   CreatedAt: %s", sub.CreatedAt.Format(time.RFC3339))
	log.Printf("   UpdatedAt: %s", sub.UpdatedAt.Format(time.RFC3339))
	if sub.ExpiresAt != nil {
		log.Printf("   ExpiresAt: %s", sub.ExpiresAt.Format(time.RFC3339))
	}
	log.Printf("   CustomerPortalURL: %s", sub.CustomerPortalURL)
	log.Printf("   UpdatePaymentMethodURL: %s", sub.UpdatePaymentMethodURL)
	log.Printf("   CustomerPortalUpdateSubscriptionURL: %s", sub.CustomerPortalUpdateSubscriptionURL)

	// Handle development mode
	if client == nil {
		log.Printf("🔧 [%s] Development mode - simulating detailed save operation", requestID)
		time.Sleep(75 * time.Millisecond) // Simulate database latency
		duration := time.Since(startTime)
		log.Printf("✅ [%s] Detailed subscription saved successfully (development mode) in %v", requestID, duration)
		return nil
	}

	// Ensure timestamps are set
	if sub.CreatedAt.IsZero() {
		sub.CreatedAt = time.Now()
		log.Printf("📅 [%s] Set creation time: %s", requestID, sub.CreatedAt.Format(time.RFC3339))
	}
	sub.UpdatedAt = time.Now()
	log.Printf("📅 [%s] Updated modification time: %s", requestID, sub.UpdatedAt.Format(time.RFC3339))

	// Marshal the subscription
	log.Printf("🔄 [%s] Marshaling detailed subscription data...", requestID)
	item, err := attributevalue.MarshalMap(sub)
	if err != nil {
		log.Printf("❌ [%s] Failed to marshal detailed subscription: %v", requestID, err)
		return fmt.Errorf("failed to marshal subscription: %w", err)
	}

	// Log the marshaled data for debugging
	log.Printf("🔍 [%s] Marshaled item contains %d attributes", requestID, len(item))

	// Prepare the put item input
	input := &dynamodb.PutItemInput{
		TableName: aws.String(TableName),
		Item:      item,
	}

	log.Printf("💾 [%s] Executing DynamoDB PutItem operation with detailed data...", requestID)
	_, err = client.PutItem(ctx, input)
	if err != nil {
		log.Printf("❌ [%s] DynamoDB PutItem failed for detailed save: %v", requestID, err)
		return fmt.Errorf("failed to save subscription: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Detailed subscription saved successfully in %v", requestID, duration)
	return nil
}

// DeleteSubscription deletes a subscription by user ID
func DeleteSubscription(ctx context.Context, userID string) error {
	startTime := time.Now()
	requestID := fmt.Sprintf("delete-%d", startTime.UnixNano())

	log.Printf("🗑️ [%s] Deleting subscription for user: %s", requestID, userID)
	log.Printf("🗑️ [%s] Table: %s", requestID, TableName)

	// Handle development mode
	if client == nil {
		log.Printf("🔧 [%s] Development mode - simulating delete operation", requestID)
		time.Sleep(30 * time.Millisecond) // Simulate database latency
		duration := time.Since(startTime)
		log.Printf("✅ [%s] Subscription deleted successfully (development mode) in %v", requestID, duration)
		return nil
	}

	// Prepare the delete item input
	input := &dynamodb.DeleteItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	}

	log.Printf("🗑️ [%s] Executing DynamoDB DeleteItem operation...", requestID)
	_, err := client.DeleteItem(ctx, input)
	if err != nil {
		log.Printf("❌ [%s] DynamoDB DeleteItem failed: %v", requestID, err)
		return fmt.Errorf("failed to delete subscription: %w", err)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Subscription deleted successfully in %v", requestID, duration)
	return nil
}

// ListSubscriptions lists all subscriptions (for admin purposes)
func ListSubscriptions(ctx context.Context, limit int32) ([]Subscription, error) {
	startTime := time.Now()
	requestID := fmt.Sprintf("list-%d", startTime.UnixNano())

	log.Printf("📋 [%s] Listing subscriptions with limit: %d", requestID, limit)
	log.Printf("📋 [%s] Table: %s", requestID, TableName)

	// Handle development mode
	if client == nil {
		log.Printf("🔧 [%s] Development mode - returning mock subscription list", requestID)
		mockSubs := []Subscription{
			{
				UserID:     "dev-user-1",
				Tier:       "plus",
				Status:     "active",
				VariantID:  887309,
				SubID:      "dev-sub-1",
				CreatedAt:  time.Now().Add(-48 * time.Hour),
				UpdatedAt:  time.Now().Add(-1 * time.Hour),
				CustomerID: "dev-customer-1",
				Email:      "dev1@example.com",
			},
			{
				UserID:     "dev-user-2",
				Tier:       "pro",
				Status:     "active",
				VariantID:  887311,
				SubID:      "dev-sub-2",
				CreatedAt:  time.Now().Add(-24 * time.Hour),
				UpdatedAt:  time.Now(),
				CustomerID: "dev-customer-2",
				Email:      "dev2@example.com",
			},
		}
		duration := time.Since(startTime)
		log.Printf("✅ [%s] Returned %d mock subscriptions in %v", requestID, len(mockSubs), duration)
		return mockSubs, nil
	}

	// Prepare the scan input
	input := &dynamodb.ScanInput{
		TableName: aws.String(TableName),
		Limit:     aws.Int32(limit),
	}

	log.Printf("📋 [%s] Executing DynamoDB Scan operation...", requestID)
	result, err := client.Scan(ctx, input)
	if err != nil {
		log.Printf("❌ [%s] DynamoDB Scan failed: %v", requestID, err)
		return nil, fmt.Errorf("failed to list subscriptions: %w", err)
	}

	// Unmarshal the results
	log.Printf("🔄 [%s] Unmarshaling %d subscription records...", requestID, len(result.Items))
	var subscriptions []Subscription
	for i, item := range result.Items {
		var sub Subscription
		err = attributevalue.UnmarshalMap(item, &sub)
		if err != nil {
			log.Printf("❌ [%s] Failed to unmarshal subscription %d: %v", requestID, i, err)
			continue
		}
		subscriptions = append(subscriptions, sub)
	}

	duration := time.Since(startTime)
	log.Printf("✅ [%s] Listed %d subscriptions successfully in %v", requestID, len(subscriptions), duration)

	// Log summary of subscriptions
	tierCounts := make(map[string]int)
	statusCounts := make(map[string]int)
	for _, sub := range subscriptions {
		tierCounts[sub.Tier]++
		statusCounts[sub.Status]++
	}

	log.Printf("📊 [%s] Subscription summary:", requestID)
	log.Printf("   Total: %d", len(subscriptions))
	for tier, count := range tierCounts {
		log.Printf("   %s tier: %d", tier, count)
	}
	for status, count := range statusCounts {
		log.Printf("   %s status: %d", status, count)
	}

	return subscriptions, nil
}

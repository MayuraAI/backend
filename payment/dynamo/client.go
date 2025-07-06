package dynamo

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

var Client *dynamodb.Client
var TableName = getEnvWithDefault("DYNAMO_TABLE", "subscriptions")

// getEnvWithDefault returns environment variable value or default if not set
func getEnvWithDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// Init initializes the DynamoDB client
func Init() error {
	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		return fmt.Errorf("failed to load AWS config: %v", err)
	}
	Client = dynamodb.NewFromConfig(cfg)
	return nil
}

// Subscription represents a user's subscription data
type Subscription struct {
	UserID                              string     `json:"user_id"`
	Tier                                string     `json:"tier"`
	VariantID                           int        `json:"variant_id"`
	Status                              string     `json:"status"`
	SubID                               string     `json:"sub_id"`
	UpdatedAt                           time.Time  `json:"updated_at"`
	CreatedAt                           time.Time  `json:"created_at"`
	ExpiresAt                           *time.Time `json:"expires_at,omitempty"`
	CustomerID                          string     `json:"customer_id,omitempty"`
	Email                               string     `json:"email,omitempty"`
	CustomerPortalURL                   string     `json:"customer_portal_url,omitempty"`
	UpdatePaymentMethodURL              string     `json:"update_payment_method_url,omitempty"`
	CustomerPortalUpdateSubscriptionURL string     `json:"customer_portal_update_subscription_url,omitempty"`
}

// SaveSubscription saves subscription data to DynamoDB
func SaveSubscription(ctx context.Context, uid string, tier string, variantID int, subID string, status string) error {
	now := time.Now()

	_, err := Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName),
		Item: map[string]types.AttributeValue{
			"user_id":    &types.AttributeValueMemberS{Value: uid},
			"tier":       &types.AttributeValueMemberS{Value: tier},
			"variant_id": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", variantID)},
			"status":     &types.AttributeValueMemberS{Value: status},
			"sub_id":     &types.AttributeValueMemberS{Value: subID},
			"updated_at": &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
			"created_at": &types.AttributeValueMemberS{Value: now.Format(time.RFC3339)},
		},
	})
	return err
}

// SaveSubscriptionDetailed saves detailed subscription data to DynamoDB
func SaveSubscriptionDetailed(ctx context.Context, sub Subscription) error {
	item := map[string]types.AttributeValue{
		"user_id":    &types.AttributeValueMemberS{Value: sub.UserID},
		"tier":       &types.AttributeValueMemberS{Value: sub.Tier},
		"variant_id": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", sub.VariantID)},
		"status":     &types.AttributeValueMemberS{Value: sub.Status},
		"sub_id":     &types.AttributeValueMemberS{Value: sub.SubID},
		"updated_at": &types.AttributeValueMemberS{Value: sub.UpdatedAt.Format(time.RFC3339)},
		"created_at": &types.AttributeValueMemberS{Value: sub.CreatedAt.Format(time.RFC3339)},
	}

	// Add optional fields
	if sub.ExpiresAt != nil {
		item["expires_at"] = &types.AttributeValueMemberS{Value: sub.ExpiresAt.Format(time.RFC3339)}
	}
	if sub.CustomerID != "" {
		item["customer_id"] = &types.AttributeValueMemberS{Value: sub.CustomerID}
	}
	if sub.Email != "" {
		item["email"] = &types.AttributeValueMemberS{Value: sub.Email}
	}
	if sub.CustomerPortalURL != "" {
		item["customer_portal_url"] = &types.AttributeValueMemberS{Value: sub.CustomerPortalURL}
	}
	if sub.UpdatePaymentMethodURL != "" {
		item["update_payment_method_url"] = &types.AttributeValueMemberS{Value: sub.UpdatePaymentMethodURL}
	}
	if sub.CustomerPortalUpdateSubscriptionURL != "" {
		item["customer_portal_update_subscription_url"] = &types.AttributeValueMemberS{Value: sub.CustomerPortalUpdateSubscriptionURL}
	}

	_, err := Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName),
		Item:      item,
	})
	return err
}

// GetSubscription retrieves subscription data from DynamoDB
func GetSubscription(ctx context.Context, uid string) (*Subscription, error) {
	out, err := Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: uid},
		},
	})
	if err != nil {
		return nil, err
	}

	if out.Item == nil {
		return nil, nil // No subscription found
	}

	// Parse the DynamoDB item into Subscription struct
	sub := &Subscription{}

	if val, ok := out.Item["user_id"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.UserID = s.Value
		}
	}

	if val, ok := out.Item["tier"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.Tier = s.Value
		}
	}

	if val, ok := out.Item["variant_id"]; ok {
		if n, ok := val.(*types.AttributeValueMemberN); ok {
			fmt.Sscanf(n.Value, "%d", &sub.VariantID)
		}
	}

	if val, ok := out.Item["status"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.Status = s.Value
		}
	}

	if val, ok := out.Item["sub_id"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.SubID = s.Value
		}
	}

	if val, ok := out.Item["updated_at"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				sub.UpdatedAt = t
			}
		}
	}

	if val, ok := out.Item["created_at"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				sub.CreatedAt = t
			}
		}
	}

	if val, ok := out.Item["expires_at"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				sub.ExpiresAt = &t
			}
		}
	}

	if val, ok := out.Item["customer_id"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.CustomerID = s.Value
		}
	}

	if val, ok := out.Item["email"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.Email = s.Value
		}
	}

	if val, ok := out.Item["customer_portal_url"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.CustomerPortalURL = s.Value
		}
	}

	if val, ok := out.Item["update_payment_method_url"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.UpdatePaymentMethodURL = s.Value
		}
	}

	if val, ok := out.Item["customer_portal_update_subscription_url"]; ok {
		if s, ok := val.(*types.AttributeValueMemberS); ok {
			sub.CustomerPortalUpdateSubscriptionURL = s.Value
		}
	}

	return sub, nil
}

// DeleteSubscription removes subscription data from DynamoDB
func DeleteSubscription(ctx context.Context, uid string) error {
	_, err := Client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: uid},
		},
	})
	return err
}

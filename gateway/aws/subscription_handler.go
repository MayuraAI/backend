package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// CreateSubscription creates a new subscription
func CreateSubscription(ctx context.Context, client *dynamodb.Client, subscription Subscription) (*Subscription, error) {
	if subscription.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	now := time.Now()
	subscription.CreatedAt = now
	subscription.UpdatedAt = now

	av, err := attributevalue.MarshalMap(subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal subscription: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(SubscriptionsTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(user_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	return &subscription, nil
}

// GetSubscription retrieves a subscription by user_id
func GetSubscription(ctx context.Context, client *dynamodb.Client, userID string) (*Subscription, error) {
	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(SubscriptionsTableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get subscription: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("subscription not found")
	}

	var subscription Subscription
	err = attributevalue.UnmarshalMap(result.Item, &subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscription: %w", err)
	}

	return &subscription, nil
}

// GetSubscriptionByUserID is an alias for GetSubscription for backward compatibility
func GetSubscriptionByUserID(ctx context.Context, client *dynamodb.Client, userID string) (*Subscription, error) {
	return GetSubscription(ctx, client, userID)
}

// UpdateSubscription updates an existing subscription
func UpdateSubscription(ctx context.Context, client *dynamodb.Client, subscription Subscription) (*Subscription, error) {
	if subscription.UserID == "" {
		return nil, fmt.Errorf("user_id is required")
	}

	subscription.UpdatedAt = time.Now()

	av, err := attributevalue.MarshalMap(subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal subscription: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(SubscriptionsTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_exists(user_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	return &subscription, nil
}

// DeleteSubscription deletes a subscription by user_id
func DeleteSubscription(ctx context.Context, client *dynamodb.Client, userID string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(SubscriptionsTableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}

	return nil
}

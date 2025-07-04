package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
)

// CreateSubscription creates a new subscription
func CreateSubscription(ctx context.Context, client *dynamodb.Client, subscription Subscription) (*Subscription, error) {
	if subscription.ID == "" {
		subscription.ID = uuid.New().String()
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
		ConditionExpression: aws.String("attribute_not_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create subscription: %w", err)
	}

	return &subscription, nil
}

// GetSubscription retrieves a subscription by ID
func GetSubscription(ctx context.Context, client *dynamodb.Client, id string) (*Subscription, error) {
	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(SubscriptionsTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
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

// GetSubscriptionByUserID retrieves a subscription by user_id using GSI
func GetSubscriptionByUserID(ctx context.Context, client *dynamodb.Client, userID string) (*Subscription, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(SubscriptionsTableName),
		IndexName:              aws.String(SubscriptionsUserIDGSI),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query subscription by user_id: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("subscription not found for user_id: %s", userID)
	}

	var subscription Subscription
	err = attributevalue.UnmarshalMap(result.Items[0], &subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscription: %w", err)
	}

	return &subscription, nil
}

// UpdateSubscription updates an existing subscription
func UpdateSubscription(ctx context.Context, client *dynamodb.Client, subscription Subscription) (*Subscription, error) {
	subscription.UpdatedAt = time.Now()

	av, err := attributevalue.MarshalMap(subscription)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal subscription: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(SubscriptionsTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update subscription: %w", err)
	}

	return &subscription, nil
}

// DeleteSubscription deletes a subscription by ID
func DeleteSubscription(ctx context.Context, client *dynamodb.Client, id string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(SubscriptionsTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}

	return nil
}

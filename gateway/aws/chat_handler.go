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

// CreateChat creates a new chat
func CreateChat(ctx context.Context, client *dynamodb.Client, chat Chat) (*Chat, error) {
	if chat.ID == "" {
		chat.ID = uuid.New().String()
	}

	now := time.Now()
	chat.CreatedAt = now
	chat.UpdatedAt = now

	// Set default sharing if not provided
	if chat.Sharing == "" {
		chat.Sharing = "private"
	}

	av, err := attributevalue.MarshalMap(chat)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(ChatsTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create chat: %w", err)
	}

	return &chat, nil
}

// GetChat retrieves a chat by ID
func GetChat(ctx context.Context, client *dynamodb.Client, id string) (*Chat, error) {
	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get chat: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("chat not found")
	}

	var chat Chat
	err = attributevalue.UnmarshalMap(result.Item, &chat)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal chat: %w", err)
	}

	return &chat, nil
}

// GetChatsByUserID retrieves all chats for a user using GSI
func GetChatsByUserID(ctx context.Context, client *dynamodb.Client, userID string) ([]Chat, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(ChatsTableName),
		IndexName:              aws.String(ChatsUserIDGSI),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query chats by user_id: %w", err)
	}

	var chats []Chat
	for _, item := range result.Items {
		var chat Chat
		err = attributevalue.UnmarshalMap(item, &chat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal chat: %w", err)
		}
		chats = append(chats, chat)
	}

	return chats, nil
}

// GetPublicChats retrieves all non-private chats
func GetPublicChats(ctx context.Context, client *dynamodb.Client) ([]Chat, error) {
	result, err := client.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(ChatsTableName),
		FilterExpression: aws.String("sharing <> :private"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":private": &types.AttributeValueMemberS{Value: "private"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to scan public chats: %w", err)
	}

	var chats []Chat
	for _, item := range result.Items {
		var chat Chat
		err = attributevalue.UnmarshalMap(item, &chat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal chat: %w", err)
		}
		chats = append(chats, chat)
	}

	return chats, nil
}

// UpdateChat updates an existing chat
func UpdateChat(ctx context.Context, client *dynamodb.Client, chat Chat) (*Chat, error) {
	chat.UpdatedAt = time.Now()

	av, err := attributevalue.MarshalMap(chat)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal chat: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(ChatsTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update chat: %w", err)
	}

	return &chat, nil
}

// DeleteChat deletes a chat by ID
func DeleteChat(ctx context.Context, client *dynamodb.Client, id string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete chat: %w", err)
	}

	return nil
}

// UpdateChatSharing updates the sharing setting of a chat
func UpdateChatSharing(ctx context.Context, client *dynamodb.Client, chatID string, sharing string) error {
	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: chatID},
		},
		UpdateExpression: aws.String("SET sharing = :sharing, updated_at = :updated_at"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sharing":    &types.AttributeValueMemberS{Value: sharing},
			":updated_at": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
		},
		ConditionExpression: aws.String("attribute_exists(id)"),
	})
	if err != nil {
		return fmt.Errorf("failed to update chat sharing: %w", err)
	}

	return nil
}

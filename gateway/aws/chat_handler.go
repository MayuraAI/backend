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
	// Step 1: Query to get the item with the given id
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String(ChatsTableName),
		KeyConditions: map[string]types.Condition{
			"id": {
				ComparisonOperator: types.ComparisonOperatorEq,
				AttributeValueList: []types.AttributeValue{
					&types.AttributeValueMemberS{Value: id},
				},
			},
		},
		Limit: aws.Int32(1), // Optional: if you're sure there's only 1 item per id
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("chat not found")
	}

	var chat Chat
	err = attributevalue.UnmarshalMap(result.Items[0], &chat)
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
	// Step 1: Query to get `created_at` for the given `id`
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String(ChatsTableName),
		KeyConditions: map[string]types.Condition{
			"id": {
				ComparisonOperator: types.ComparisonOperatorEq,
				AttributeValueList: []types.AttributeValue{
					&types.AttributeValueMemberS{Value: chat.ID},
				},
			},
		},
		Limit: aws.Int32(1), // Optional: if you're sure there's only 1 item per id
	})
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("no item found with id: %s", chat.ID)
	}

	// Extract created_at from the result
	createdAt := result.Items[0]["created_at"].(*types.AttributeValueMemberS).Value

	// Step 2: Update the chat with both id and created_at
	chat.UpdatedAt = time.Now()

	// Build update expression and attribute values dynamically
	updateExpression := "SET updated_at = :updated_at"
	expressionAttributeValues := map[string]types.AttributeValue{
		":updated_at": &types.AttributeValueMemberS{Value: chat.UpdatedAt.Format(time.RFC3339)},
	}
	expressionAttributeNames := map[string]string{}

	// Add other fields to update if they are not empty
	if chat.Name != "" {
		updateExpression += ", #name = :name"
		expressionAttributeValues[":name"] = &types.AttributeValueMemberS{Value: chat.Name}
		expressionAttributeNames["#name"] = "name"
	}
	if chat.UserID != "" {
		updateExpression += ", user_id = :user_id"
		expressionAttributeValues[":user_id"] = &types.AttributeValueMemberS{Value: chat.UserID}
	}
	if chat.Sharing != "" {
		updateExpression += ", sharing = :sharing"
		expressionAttributeValues[":sharing"] = &types.AttributeValueMemberS{Value: chat.Sharing}
	}

	updateInput := &dynamodb.UpdateItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id":         &types.AttributeValueMemberS{Value: chat.ID},
			"created_at": &types.AttributeValueMemberS{Value: createdAt},
		},
		UpdateExpression:          aws.String(updateExpression),
		ExpressionAttributeValues: expressionAttributeValues,
		ConditionExpression:       aws.String("attribute_exists(id)"),
	}

	// Only add ExpressionAttributeNames if we have any
	if len(expressionAttributeNames) > 0 {
		updateInput.ExpressionAttributeNames = expressionAttributeNames
	}

	_, err = client.UpdateItem(ctx, updateInput)
	if err != nil {
		return nil, fmt.Errorf("failed to update chat: %w", err)
	}

	// Set the original created_at value for the returned chat
	if parsedTime, parseErr := time.Parse(time.RFC3339, createdAt); parseErr == nil {
		chat.CreatedAt = parsedTime
	}

	return &chat, nil
}

// DeleteChat deletes a chat by ID
func DeleteChat(ctx context.Context, client *dynamodb.Client, id string) error {
	// Step 1: Query to get `created_at` for the given `id`
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String(ChatsTableName),
		KeyConditions: map[string]types.Condition{
			"id": {
				ComparisonOperator: types.ComparisonOperatorEq,
				AttributeValueList: []types.AttributeValue{
					&types.AttributeValueMemberS{Value: id},
				},
			},
		},
		Limit: aws.Int32(1), // Optional: if you're sure there's only 1 item per id
	})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if len(result.Items) == 0 {
		return fmt.Errorf("no item found with id: %s", id)
	}

	// Extract created_at from the result
	createdAt := result.Items[0]["created_at"].(*types.AttributeValueMemberS).Value

	// Step 2: Delete with both id and created_at
	_, err = client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id":         &types.AttributeValueMemberS{Value: id},
			"created_at": &types.AttributeValueMemberS{Value: createdAt},
		},
	})
	if err != nil {
		return fmt.Errorf("delete failed: %w", err)
	}
	return nil
}

// UpdateChatSharing updates the sharing setting of a chat
func UpdateChatSharing(ctx context.Context, client *dynamodb.Client, chatID string, sharing string) error {
	// Step 1: Query to get `created_at` for the given `id`
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName: aws.String(ChatsTableName),
		KeyConditions: map[string]types.Condition{
			"id": {
				ComparisonOperator: types.ComparisonOperatorEq,
				AttributeValueList: []types.AttributeValue{
					&types.AttributeValueMemberS{Value: chatID},
				},
			},
		},
		Limit: aws.Int32(1), // Optional: if you're sure there's only 1 item per id
	})
	if err != nil {
		return fmt.Errorf("query failed: %w", err)
	}

	if len(result.Items) == 0 {
		return fmt.Errorf("no item found with id: %s", chatID)
	}

	// Extract created_at from the result
	createdAt := result.Items[0]["created_at"].(*types.AttributeValueMemberS).Value

	// Step 2: Update with both id and created_at
	_, err = client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(ChatsTableName),
		Key: map[string]types.AttributeValue{
			"id":         &types.AttributeValueMemberS{Value: chatID},
			"created_at": &types.AttributeValueMemberS{Value: createdAt},
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

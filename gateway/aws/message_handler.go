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

// CreateMessage creates a new message
func CreateMessage(ctx context.Context, client *dynamodb.Client, message Message) (*Message, error) {
	if message.ID == "" {
		message.ID = uuid.New().String()
	}

	now := time.Now()
	message.CreatedAt = now
	message.UpdatedAt = now

	av, err := attributevalue.MarshalMap(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(MessagesTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	return &message, nil
}

// GetMessage retrieves a message by ID
func GetMessage(ctx context.Context, client *dynamodb.Client, id string) (*Message, error) {
	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(MessagesTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get message: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("message not found")
	}

	var message Message
	err = attributevalue.UnmarshalMap(result.Item, &message)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal message: %w", err)
	}

	return &message, nil
}

// GetMessagesByChatID retrieves all messages for a chat using GSI
func GetMessagesByChatID(ctx context.Context, client *dynamodb.Client, chatID string) ([]Message, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(MessagesTableName),
		IndexName:              aws.String(MessagesChatIDGSI),
		KeyConditionExpression: aws.String("chat_id = :chat_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":chat_id": &types.AttributeValueMemberS{Value: chatID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query messages by chat_id: %w", err)
	}

	var messages []Message
	for _, item := range result.Items {
		var message Message
		err = attributevalue.UnmarshalMap(item, &message)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// GetMessagesByUserID retrieves all messages for a user using GSI
func GetMessagesByUserID(ctx context.Context, client *dynamodb.Client, userID string) ([]Message, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(MessagesTableName),
		IndexName:              aws.String(MessagesUserIDGSI),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query messages by user_id: %w", err)
	}

	var messages []Message
	for _, item := range result.Items {
		var message Message
		err = attributevalue.UnmarshalMap(item, &message)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// GetMessagesForPublicChats retrieves messages for non-private chats
func GetMessagesForPublicChats(ctx context.Context, client *dynamodb.Client, chatIDs []string) ([]Message, error) {
	var messages []Message

	for _, chatID := range chatIDs {
		chatMessages, err := GetMessagesByChatID(ctx, client, chatID)
		if err != nil {
			return nil, fmt.Errorf("failed to get messages for chat %s: %w", chatID, err)
		}
		messages = append(messages, chatMessages...)
	}

	return messages, nil
}

// UpdateMessage updates an existing message
func UpdateMessage(ctx context.Context, client *dynamodb.Client, message Message) (*Message, error) {
	message.UpdatedAt = time.Now()

	av, err := attributevalue.MarshalMap(message)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(MessagesTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_exists(id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update message: %w", err)
	}

	return &message, nil
}

// DeleteMessage deletes a message by ID
func DeleteMessage(ctx context.Context, client *dynamodb.Client, id string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(MessagesTableName),
		Key: map[string]types.AttributeValue{
			"id": &types.AttributeValueMemberS{Value: id},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete message: %w", err)
	}

	return nil
}

// DeleteMessagesIncludingAndAfter deletes messages with sequence number >= specified number
// This replicates the Supabase function delete_messages_including_and_after
func DeleteMessagesIncludingAndAfter(ctx context.Context, client *dynamodb.Client, userID, chatID string, sequenceNumber int) error {
	// First, get all messages for the chat
	messages, err := GetMessagesByChatID(ctx, client, chatID)
	if err != nil {
		return fmt.Errorf("failed to get messages for chat: %w", err)
	}

	// Filter messages that belong to the user and have sequence number >= specified
	var messagesToDelete []Message
	for _, message := range messages {
		if message.UserID == userID && message.SequenceNumber >= sequenceNumber {
			messagesToDelete = append(messagesToDelete, message)
		}
	}

	// Delete each message
	for _, message := range messagesToDelete {
		err = DeleteMessage(ctx, client, message.ID)
		if err != nil {
			return fmt.Errorf("failed to delete message %s: %w", message.ID, err)
		}
	}

	return nil
}

// GetMessagesByChatIDAndSequenceRange retrieves messages within a sequence number range
func GetMessagesByChatIDAndSequenceRange(ctx context.Context, client *dynamodb.Client, chatID string, minSequence, maxSequence int) ([]Message, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(MessagesTableName),
		IndexName:              aws.String(MessagesChatIDGSI),
		KeyConditionExpression: aws.String("chat_id = :chat_id"),
		FilterExpression:       aws.String("sequence_number BETWEEN :min_seq AND :max_seq"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":chat_id": &types.AttributeValueMemberS{Value: chatID},
			":min_seq": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", minSequence)},
			":max_seq": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", maxSequence)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query messages by sequence range: %w", err)
	}

	var messages []Message
	for _, item := range result.Items {
		var message Message
		err = attributevalue.UnmarshalMap(item, &message)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal message: %w", err)
		}
		messages = append(messages, message)
	}

	return messages, nil
}

// GetNextSequenceNumber gets the next sequence number for a chat
func GetNextSequenceNumber(ctx context.Context, client *dynamodb.Client, chatID string) (int, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(MessagesTableName),
		IndexName:              aws.String(MessagesChatIDGSI),
		KeyConditionExpression: aws.String("chat_id = :chat_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":chat_id": &types.AttributeValueMemberS{Value: chatID},
		},
		ScanIndexForward: aws.Bool(false), // Descending order
		Limit:            aws.Int32(1),    // Get only the latest message
	})
	if err != nil {
		return 0, fmt.Errorf("failed to query latest message: %w", err)
	}

	if len(result.Items) == 0 {
		return 1, nil // First message
	}

	var latestMessage Message
	err = attributevalue.UnmarshalMap(result.Items[0], &latestMessage)
	if err != nil {
		return 0, fmt.Errorf("failed to unmarshal latest message: %w", err)
	}

	return latestMessage.SequenceNumber + 1, nil
}

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

// CreateProfile creates a new profile
func CreateProfile(ctx context.Context, client *dynamodb.Client, profile Profile) (*Profile, error) {
	now := time.Now()
	profile.CreatedAt = now
	profile.UpdatedAt = now

	av, err := attributevalue.MarshalMap(profile)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(ProfilesTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_not_exists(user_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create profile: %w", err)
	}

	return &profile, nil
}

// GetProfile retrieves a profile by ID
func GetProfile(ctx context.Context, client *dynamodb.Client, userID string) (*Profile, error) {
	result, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(ProfilesTableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get profile: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("profile not found")
	}

	var profile Profile
	err = attributevalue.UnmarshalMap(result.Item, &profile)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &profile, nil
}

// GetProfileByUserID retrieves a profile by user_id using GSI
func GetProfileByUserID(ctx context.Context, client *dynamodb.Client, userID string) (*Profile, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(ProfilesTableName),
		IndexName:              aws.String(ProfilesUserIDGSI),
		KeyConditionExpression: aws.String("user_id = :user_id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query profile by user_id: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("profile not found for user_id: %s", userID)
	}

	var profile Profile
	err = attributevalue.UnmarshalMap(result.Items[0], &profile)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &profile, nil
}

// GetProfileByUsername retrieves a profile by username using GSI
func GetProfileByUsername(ctx context.Context, client *dynamodb.Client, username string) (*Profile, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(ProfilesTableName),
		IndexName:              aws.String(ProfilesUsernameGSI),
		KeyConditionExpression: aws.String("username = :username"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":username": &types.AttributeValueMemberS{Value: username},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to query profile by username: %w", err)
	}

	if len(result.Items) == 0 {
		return nil, fmt.Errorf("profile not found for username: %s", username)
	}

	var profile Profile
	err = attributevalue.UnmarshalMap(result.Items[0], &profile)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal profile: %w", err)
	}

	return &profile, nil
}

// UpdateProfile updates an existing profile
func UpdateProfile(ctx context.Context, client *dynamodb.Client, profile Profile) (*Profile, error) {
	profile.UpdatedAt = time.Now()

	av, err := attributevalue.MarshalMap(profile)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal profile: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(ProfilesTableName),
		Item:                av,
		ConditionExpression: aws.String("attribute_exists(user_id)"),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update profile: %w", err)
	}

	return &profile, nil
}

// DeleteProfile deletes a profile by ID
func DeleteProfile(ctx context.Context, client *dynamodb.Client, userID string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(ProfilesTableName),
		Key: map[string]types.AttributeValue{
			"user_id": &types.AttributeValueMemberS{Value: userID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete profile: %w", err)
	}

	return nil
}

// CheckUsernameAvailable checks if a username is available (not taken by another user)
func CheckUsernameAvailable(ctx context.Context, client *dynamodb.Client, username string, excludeUserID string) (bool, error) {
	result, err := client.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(ProfilesTableName),
		IndexName:              aws.String(ProfilesUsernameGSI),
		KeyConditionExpression: aws.String("username = :username"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":username": &types.AttributeValueMemberS{Value: username},
		},
	})
	if err != nil {
		return false, fmt.Errorf("failed to check username availability: %w", err)
	}

	// If no results, username is available
	if len(result.Items) == 0 {
		return true, nil
	}

	// If there's a result, check if it belongs to the excluded user
	if excludeUserID != "" {
		var profile Profile
		err = attributevalue.UnmarshalMap(result.Items[0], &profile)
		if err != nil {
			return false, fmt.Errorf("failed to unmarshal profile: %w", err)
		}

		// Username is available if it belongs to the excluded user
		return profile.UserID == excludeUserID, nil
	}

	// Username is taken
	return false, nil
}

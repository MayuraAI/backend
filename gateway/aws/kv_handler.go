package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

type Kv struct {
	Key   string `json:"key"`
	Value string `json:"value"` // could be int
}

const TableName = "kv-store"

func PutKv(ctx context.Context, client *dynamodb.Client, kv Kv) error {
	av, err := attributevalue.MarshalMap(kv)
	if err != nil {
		return err
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(TableName),
		Item:      av,
	})

	return err
}

func GetKv(ctx context.Context, client *dynamodb.Client, key string) (Kv, error) {
	av, err := client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"Key": &types.AttributeValueMemberS{Value: key},
		},
	})
	if err != nil {
		return Kv{}, err
	}

	if av.Item == nil {
		return Kv{}, fmt.Errorf("item not found")
	}

	kv := Kv{}
	err = attributevalue.UnmarshalMap(av.Item, &kv)
	if err != nil {
		return Kv{}, err
	}

	return kv, nil
}

func UpdateKv(ctx context.Context, client *dynamodb.Client, kv Kv) error {
	_, err := client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"Key": &types.AttributeValueMemberS{Value: kv.Key},
		},
	})

	return err
}

func DeleteKv(ctx context.Context, client *dynamodb.Client, key string) error {
	_, err := client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(TableName),
		Key: map[string]types.AttributeValue{
			"Key": &types.AttributeValueMemberS{Value: key},
		},
	})

	return err
}

package redis

import (
	"context"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	// Global Redis client instance
	client *redis.Client
)

// InitRedis initializes the Redis client with the given URL
func InitRedis(redisURL string) error {
	if redisURL == "" {
		redisURL = os.Getenv("REDIS_URL")
		if redisURL == "" {
			redisURL = "redis://localhost:6379"
		}
	}

	// Parse Redis URL
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return err
	}

	// Create Redis client
	client = redis.NewClient(opt)

	// Test connection
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = client.Ping(ctx).Result()
	if err != nil {
		return err
	}

	return nil
}

// GetClient returns the Redis client instance
func GetClient() *redis.Client {
	return client
}

// Close closes the Redis client connection
func Close() error {
	if client != nil {
		return client.Close()
	}
	return nil
}

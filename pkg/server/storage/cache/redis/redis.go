package redis

import (
	"context"
	"fmt"
	"interestBar/pkg/conf"
	"interestBar/pkg/logger"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	Client *redis.Client
	Ctx    = context.Background()
)

// InitRedis initializes Redis connection
func InitRedis() error {
	Client = redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", conf.Config.Redis.Host, conf.Config.Redis.Port),
		Password: conf.Config.Redis.Password,
		DB:       conf.Config.Redis.D,
		PoolSize: conf.Config.Redis.PoolSize,
	})

	// Test connection
	_, err := Client.Ping(Ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Log.Info("Redis connected successfully")
	return nil
}

// SetToken stores token in Redis with expiration
func SetToken(token string, userID uint, expiration time.Duration) error {
	key := fmt.Sprintf("auth:token:%s", token)
	return Client.Set(Ctx, key, userID, expiration).Err()
}

// GetToken retrieves user ID from Redis by token
func GetToken(token string) (uint, error) {
	key := fmt.Sprintf("auth:token:%s", token)
	userID, err := Client.Get(Ctx, key).Uint64()
	if err != nil {
		return 0, err
	}
	return uint(userID), nil
}

// DeleteToken removes token from Redis (logout)
func DeleteToken(token string) error {
	key := fmt.Sprintf("auth:token:%s", token)
	return Client.Del(Ctx, key).Err()
}

// SetUserSession stores user session data
func SetUserSession(userID uint, data map[string]interface{}, expiration time.Duration) error {
	key := fmt.Sprintf("auth:session:%d", userID)
	return Client.HMSet(Ctx, key, data).Err()
}

// GetUserSession retrieves user session data
func GetUserSession(userID uint) (map[string]string, error) {
	key := fmt.Sprintf("auth:session:%d", userID)
	return Client.HGetAll(Ctx, key).Result()
}

// DeleteUserSession removes user session
func DeleteUserSession(userID uint) error {
	key := fmt.Sprintf("auth:session:%d", userID)
	return Client.Del(Ctx, key).Err()
}

// DeleteAllUserTokens deletes all tokens for a specific user (session invalidation)
func DeleteAllUserTokens(userID uint) error {
	// Scan for all keys matching the pattern auth:token:*
	iter := Client.Scan(Ctx, 0, "auth:token:*", 100).Iterator()
	var keysToDelete []string

	for iter.Next(Ctx) {
		key := iter.Val()
		// Get the userID stored in this token
		storedUserID, err := Client.Get(Ctx, key).Uint64()
		if err == nil && uint(storedUserID) == userID {
			keysToDelete = append(keysToDelete, key)
		}
	}

	if err := iter.Err(); err != nil {
		return err
	}

	// Delete all matching keys
	if len(keysToDelete) > 0 {
		return Client.Del(Ctx, keysToDelete...).Err()
	}

	return nil
}

// DeleteAllUserTokensExceptCurrent deletes all user tokens except the current one
func DeleteAllUserTokensExceptCurrent(userID uint, currentToken string) error {
	// Scan for all keys matching the pattern auth:token:*
	iter := Client.Scan(Ctx, 0, "auth:token:*", 100).Iterator()
	var keysToDelete []string

	for iter.Next(Ctx) {
		key := iter.Val()
		// Skip the current token
		if key == fmt.Sprintf("auth:token:%s", currentToken) {
			continue
		}

		// Get the userID stored in this token
		storedUserID, err := Client.Get(Ctx, key).Uint64()
		if err == nil && uint(storedUserID) == userID {
			keysToDelete = append(keysToDelete, key)
		}
	}

	if err := iter.Err(); err != nil {
		return err
	}

	// Delete all matching keys
	if len(keysToDelete) > 0 {
		return Client.Del(Ctx, keysToDelete...).Err()
	}

	return nil
}

// GetUserActiveTokensCount returns the number of active tokens for a user
func GetUserActiveTokensCount(userID uint) (int, error) {
	iter := Client.Scan(Ctx, 0, "auth:token:*", 100).Iterator()
	count := 0

	for iter.Next(Ctx) {
		key := iter.Val()
		storedUserID, err := Client.Get(Ctx, key).Uint64()
		if err == nil && uint(storedUserID) == userID {
			count++
		}
	}

	if err := iter.Err(); err != nil {
		return 0, err
	}

	return count, nil
}

// Close closes Redis connection
func Close() error {
	if Client != nil {
		return Client.Close()
	}
	return nil
}

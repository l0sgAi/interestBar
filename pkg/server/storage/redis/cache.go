package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"interestBar/pkg/logger"

	"github.com/klauspost/compress/zstd"
	"github.com/redis/go-redis/v9"
)

var (
	// Client Redis客户端实例
	Client *redis.Client
	ctx    = context.Background()
)

// InitRedis 初始化Redis连接
func InitRedis(addr, password string, db int) error {
	Client = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	// 测试连接
	_, err := Client.Ping(ctx).Result()
	if err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}

	return nil
}

// CloseRedis 关闭Redis连接
func CloseRedis() error {
	if Client != nil {
		return Client.Close()
	}
	return nil
}

// Set 设置键值对
func Set(key string, value interface{}, expiration time.Duration) error {
	return Client.Set(ctx, key, value, expiration).Err()
}

// Get 获取键值
func Get(key string) (string, error) {
	return Client.Get(ctx, key).Result()
}

// Del 删除键
func Del(keys ...string) error {
	return Client.Del(ctx, keys...).Err()
}

// Exists 检查键是否存在
func Exists(keys ...string) (int64, error) {
	return Client.Exists(ctx, keys...).Result()
}

// Expire 设置键的过期时间
func Expire(key string, expiration time.Duration) error {
	return Client.Expire(ctx, key, expiration).Err()
}

// SetJSON 设置JSON对象（自动序列化）
func SetJSON(key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return Client.Set(ctx, key, data, expiration).Err()
}

// GetJSON 获取JSON对象（自动反序列化）
func GetJSON(key string, dest interface{}) error {
	data, err := Client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

// BatchSet 批量设置键值对（使用 Pipeline）
// keyValues 是一个 map，key 是 Redis key，value 是要设置的值
func BatchSet(keyValues map[string]interface{}, expiration time.Duration) error {
	if len(keyValues) == 0 {
		return nil
	}

	pipe := Client.Pipeline()
	for key, value := range keyValues {
		pipe.Set(ctx, key, value, expiration)
	}

	_, err := pipe.Exec(ctx)
	if err != nil {
		logger.Log.Warn(
			fmt.Sprintf("Redis BatchSet failed, keyCount=%d, err=%v",
				len(keyValues), err,
			),
		)
	}
	return err
}

// Incr 原子递增键值，并设置过期时间
// 如果键不存在则从0开始递增
// 返回递增后的值
func Incr(key string, expiration time.Duration) (int64, error) {
	pipe := Client.Pipeline()
	incrCmd := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, expiration)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to increment key %s: %w", key, err)
	}

	return incrCmd.Result()
}

// Decr 原子递减键值，并设置过期时间
// 如果键不存在则从0开始递减（结果为-1，调用方需要处理）
// 返回递减后的值（可能为负数）
func Decr(key string, expiration time.Duration) (int64, error) {
	pipe := Client.Pipeline()
	decrCmd := pipe.Decr(ctx, key)
	pipe.Expire(ctx, key, expiration)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to decrement key %s: %w", key, err)
	}

	result, err := decrCmd.Result()
	if err != nil {
		return 0, err
	}

	// 确保不会小于0
	if result < 0 {
		// 如果小于0，重置为0
		err = Client.Set(ctx, key, 0, expiration).Err()
		if err != nil {
			return 0, fmt.Errorf("failed to reset negative count: %w", err)
		}
		return 0, nil
	}

	return result, nil
}

var (
	// zstdEncoder Zstd 编码器复用（性能优化）
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedFastest))
	// zstdDecoder Zstd 解码器复用
	zstdDecoder, _ = zstd.NewReader(nil)
)

// compressZstd 使用 Zstd 压缩数据
func compressZstd(data []byte) []byte {
	return zstdEncoder.EncodeAll(data, make([]byte, 0, len(data)))
}

// decompressZstd 使用 Zstd 解压数据
func decompressZstd(data []byte) ([]byte, error) {
	return zstdDecoder.DecodeAll(data, nil)
}

// SetJSONCompressed 将对象序列化为JSON并压缩后存储到Redis
func SetJSONCompressed(key string, value interface{}, expiration time.Duration) error {
	// 1. 序列化为 JSON
	jsonData, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// 2. Zstd 压缩
	compressed := compressZstd(jsonData)

	// 3. 存储到 Redis
	err = Client.Set(ctx, key, compressed, expiration).Err()
	if err != nil {
		return fmt.Errorf("failed to set compressed data: %w", err)
	}

	// 记录压缩效果
	if len(jsonData) > 1024 { // 只对大于 1KB 的数据记录日志
		logger.Log.Debug(
			fmt.Sprintf("Zstd compressed: key=%s, original=%d, compressed=%d, ratio=%.2f%%",
				key, len(jsonData), len(compressed),
				float64(len(compressed))/float64(len(jsonData))*100,
			),
		)
	}

	return nil
}

// GetJSONCompressed 从Redis获取压缩数据，解压并反序列化为对象
func GetJSONCompressed(key string, value interface{}) error {
	// 1. 获取压缩数据
	compressed, err := Client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return err // 键不存在
		}
		return fmt.Errorf("failed to get compressed data: %w", err)
	}

	// 2. Zstd 解压
	jsonData, err := decompressZstd(compressed)
	if err != nil {
		return fmt.Errorf("failed to decompress data: %w", err)
	}

	// 3. 反序列化 JSON
	if err := json.Unmarshal(jsonData, value); err != nil {
		return fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return nil
}

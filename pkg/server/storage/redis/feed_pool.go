package redis

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// feed:recommend:{user_id} 候选池（LIST）+ feed:recommend:token:{user_id}（版本 token）。
//
// 池由 RecommendService 在 miss/TTL 过期时重建：5 路召回 → 交错合并 → RPUSH 有序 post_id。
// 翻页用 LRANGE offset；token 供客户端回传比对，不一致则池已重建 → 回 offset=0（防翻页错位）。

// BuildFeedPool 原子重建推荐流候选池：DEL 旧池 → RPUSH 有序 IDs → 写版本 token，池与 token 同 TTL。
// 返回新建的 token。ids 为空时仍建空池（调用方按需降级）。
func BuildFeedPool(userID uuid.UUID, ids []string, ttl time.Duration) (string, error) {
	token := uuid.NewString()
	listKey := GetRecommendFeedKey(userID)
	tokenKey := GetRecommendFeedTokenKey(userID)

	pipe := Client.TxPipeline()
	pipe.Del(ctx, listKey)
	if len(ids) > 0 {
		vals := make([]interface{}, 0, len(ids))
		for _, id := range ids {
			vals = append(vals, id)
		}
		pipe.RPush(ctx, listKey, vals...)
	}
	pipe.Set(ctx, tokenKey, token, ttl) // token 自带 TTL
	pipe.Expire(ctx, listKey, ttl)      // list 需单独 EXPIRE
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("failed to build feed pool: %w", err)
	}
	return token, nil
}

// FeedPoolExists 候选池 LIST 是否存在（未过 TTL）。
func FeedPoolExists(userID uuid.UUID) (bool, error) {
	n, err := Client.Exists(ctx, GetRecommendFeedKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// FeedPoolLen 候选池当前长度（LLEN）。
func FeedPoolLen(userID uuid.UUID) (int64, error) {
	return Client.LLen(ctx, GetRecommendFeedKey(userID)).Result()
}

// FeedPoolRange 取候选池 [offset, offset+size-1] 区间的 postID（LRANGE）。
func FeedPoolRange(userID uuid.UUID, offset, size int64) ([]string, error) {
	if size <= 0 {
		return nil, nil
	}
	return Client.LRange(ctx, GetRecommendFeedKey(userID), offset, offset+size-1).Result()
}

// GetFeedPoolToken 读取候选池版本 token；池不存在时返回 ""（触发调用方按 miss 处理）。
func GetFeedPoolToken(userID uuid.UUID) (string, error) {
	t, err := Client.Get(ctx, GetRecommendFeedTokenKey(userID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return t, nil
}

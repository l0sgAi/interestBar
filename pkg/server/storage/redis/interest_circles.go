package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// user:interest_circles:{user_id} —— C3 行为圈子缓存（SET）。
//
// 由 recommend 域 recall 在 miss 时从 DB（seed 帖子 → circle_id）反查并落缓存，
// 避免每轮 recall 都查 DB。读路径在内存中减去 joined circles 得 C3 候选圈。
// 兴趣随点赞/收藏漂移，TTL 可配（默认 2h）。

// InterestCirclesExists 行为兴趣圈子 SET 是否存在（区分 miss 与空集）。
func InterestCirclesExists(userID uuid.UUID) (bool, error) {
	n, err := Client.Exists(ctx, GetUserInterestCirclesKey(userID)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetInterestCircles 读取行为兴趣圈子 SET 的全部 circleID（SMEMBERS）。未命中返回空。
func GetInterestCircles(userID uuid.UUID) ([]string, error) {
	members, err := Client.SMembers(ctx, GetUserInterestCirclesKey(userID)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get interest circles: %w", err)
	}
	return members, nil
}

// SetInterestCircles 覆盖式写入行为兴趣圈子 SET（DEL + SADD + EXPIRE）。
func SetInterestCircles(userID uuid.UUID, circleIDs []uuid.UUID, ttl time.Duration) error {
	key := GetUserInterestCirclesKey(userID)
	pipe := Client.TxPipeline()
	pipe.Del(ctx, key)
	if len(circleIDs) > 0 {
		vals := make([]interface{}, 0, len(circleIDs))
		for _, c := range circleIDs {
			vals = append(vals, c.String())
		}
		pipe.SAdd(ctx, key, vals...)
	}
	pipe.Expire(ctx, key, ttl)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("failed to set interest circles: %w", err)
	}
	return nil
}

package infrastructure

import (
	"context"
	"strconv"

	"interestBar/pkg/domains/notice/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// noticeUnreadCacheRedis 基于 Redis 的 NoticeUnreadCache 实现。
//
// 直接使用 redispkg.Client（与 like_lua.go 等同包先例一致），key 用 constants 的 helper。
type noticeUnreadCacheRedis struct{}

// NewNoticeUnreadCache 构造 NoticeUnreadCache。
func NewNoticeUnreadCache() domain.NoticeUnreadCache {
	return &noticeUnreadCacheRedis{}
}

// Get 读取未读计数。miss（key 不存在）返回 ok=false。
func (c *noticeUnreadCacheRedis) Get(ctx context.Context, userID uuid.UUID) (int64, bool, error) {
	val, err := redispkg.Client.Get(ctx, redispkg.GetNoticeUnreadKey(userID)).Result()
	if err == redis.Nil {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	count, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false, nil // 脏数据视同 miss，回源校正
	}
	return count, true, nil
}

// IncrBy 累加未读计数（key 不存在时从 0 起，顺带设 TTL）。
func (c *noticeUnreadCacheRedis) IncrBy(ctx context.Context, userID uuid.UUID, delta int64) error {
	key := redispkg.GetNoticeUnreadKey(userID)
	pipe := redispkg.Client.Pipeline()
	pipe.IncrBy(ctx, key, delta)
	pipe.Expire(ctx, key, redispkg.NoticeUnreadTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// decrFloorZeroScript 原子扣减并 floor 0；key 不存在返回 -1（调用方不动作）。
const decrFloorZeroScript = `
local cur = redis.call('GET', KEYS[1])
if not cur then
    return -1
end
local next = tonumber(cur) - tonumber(ARGV[1])
if next < 0 then
    next = 0
end
redis.call('SET', KEYS[1], next, 'KEEPTTL')
return next
`

// DecrBy 扣减未读计数（floor 0）；key 不存在时不动作（等读 miss 回源校正）。
func (c *noticeUnreadCacheRedis) DecrBy(ctx context.Context, userID uuid.UUID, delta int64) error {
	_, err := redispkg.Client.Eval(ctx, decrFloorZeroScript, []string{redispkg.GetNoticeUnreadKey(userID)}, delta).Result()
	if err == redis.Nil {
		return nil
	}
	return err
}

// Set 直接设置未读计数（回源回填 / read-all 置 0）。
func (c *noticeUnreadCacheRedis) Set(ctx context.Context, userID uuid.UUID, count int64) error {
	return redispkg.Client.Set(ctx, redispkg.GetNoticeUnreadKey(userID), count, redispkg.NoticeUnreadTTL).Err()
}

package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// historyRecordScript 浏览历史记录原子脚本。
//
// 与 collectToggleScript 同构,但 history 是单向记录(无 toggle/ZREM/stats Hash 交互):
//   - ZADD upsert:已存在则更新 score(移到「最近浏览」顶部),不存在则插入;
//   - 超限 trim:仅保留最近 maxSize 条(按 score 降序),淘汰最低分(最久未访);
//   - 续期 TTL(复用 postStatsTTL,与 collect/like ZSET 一致)。
const historyRecordScript = `
local zsetKey = KEYS[1]
local postId  = ARGV[1]
local now     = tonumber(ARGV[2])
local maxSize = tonumber(ARGV[3])
local ttl     = tonumber(ARGV[4])

-- upsert:更新 score(移到顶部) 或插入
redis.call('ZADD', zsetKey, now, postId)

-- trim:仅保留 score 最高的 maxSize 条,淘汰最低分
local size = redis.call('ZCARD', zsetKey)
if tonumber(size) > maxSize then
    local removeCount = tonumber(size) - maxSize
    redis.call('ZREMRANGEBYRANK', zsetKey, 0, removeCount - 1)
end

redis.call('EXPIRE', zsetKey, ttl)
return 1
`

var historyRecordSHA string

// historyMaxZsetSize 浏览历史 ZSET 容量上限(用户要求 500 条)。
const historyMaxZsetSize int64 = 500

// InitHistoryLuaScripts 预加载浏览历史 Lua 脚本到 Redis(启动时调用)。
func InitHistoryLuaScripts() error {
	var err error
	historyRecordSHA, err = Client.ScriptLoad(ctx, historyRecordScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load history record script: %w", err)
	}
	return nil
}

// RecordPostView 原子记录一次浏览(ZADD upsert + trim 500 + EXPIRE)。
// score=当前时间戳(Unix 毫秒),即「最近访问时间」;重复浏览同帖则 bump score 移到顶部。
func RecordPostView(userID, postID uuid.UUID) error {
	zsetKey := GetUserPostViewListKey(userID)
	now := time.Now().UnixMilli()
	ttlSeconds := int64(postStatsTTL.Seconds())

	_, err := Client.EvalSha(ctx, historyRecordSHA,
		[]string{zsetKey},
		postID.String(), now, historyMaxZsetSize, ttlSeconds,
	).Int64()

	if err != nil {
		// Redis 重启后 SHA 失效,重新加载并重试(与 collect/like 一致)
		historyRecordSHA, err = Client.ScriptLoad(ctx, historyRecordScript).Result()
		if err != nil {
			return fmt.Errorf("failed to reload history record script: %w", err)
		}
		_, err = Client.EvalSha(ctx, historyRecordSHA,
			[]string{zsetKey},
			postID.String(), now, historyMaxZsetSize, ttlSeconds,
		).Int64()
		if err != nil {
			return fmt.Errorf("failed to execute history record: %w", err)
		}
	}
	return nil
}

// ListPostViews 倒序取浏览历史 ZSET 的 [offset, offset+size-1] 区间 postID。
// 返回 postID 字符串列表(按最近访问时间倒序)、总数(ZCARD)、error。
// 用于「最近浏览」列表 offset 分页。
func ListPostViews(userID uuid.UUID, offset, size int64) ([]string, int64, error) {
	zsetKey := GetUserPostViewListKey(userID)

	pipe := Client.Pipeline()
	zrevRange := pipe.ZRevRange(ctx, zsetKey, offset, offset+size-1)
	zcard := pipe.ZCard(ctx, zsetKey)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, 0, fmt.Errorf("failed to list post views: %w", err)
	}

	members, err := zrevRange.Result()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get post view members: %w", err)
	}
	total, err := zcard.Result()
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get post view total: %w", err)
	}
	return members, total, nil
}

// BackfillPostViews 将 DB 回源确认的浏览历史 postID 批量回填到 ZSET。
// 用于 ZSET 冷启动(ZCARD==0)时从 DB top500 恢复。
//
// postIDs 须已按 update_time DESC 排序(由 repo 保证),这里按 index 递减分配 score,
// 使回填后 ZSET 的 score 倒序与 DB 排序一致;后续真实浏览(score=now)会自然冒泡到顶部。
func BackfillPostViews(userID uuid.UUID, postIDs []uuid.UUID) error {
	if len(postIDs) == 0 {
		return nil
	}
	zsetKey := GetUserPostViewListKey(userID)
	base := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(postIDs))
	for i, id := range postIDs {
		// index 越大(越久未访)score 越小,保证倒序与入参顺序一致
		members[i] = redis.Z{Score: base - float64(i), Member: id.String()}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

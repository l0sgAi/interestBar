package redis

import (
	"fmt"

	"github.com/google/uuid"
)

const viewIncrScript = `
local statsKey = KEYS[1]
local dedupeKey = KEYS[2]
local maxViews = tonumber(ARGV[1])
local dedupeTTL = tonumber(ARGV[2])
local statsTTL = tonumber(ARGV[3])

-- 1. 去重检查：同一用户短时间内已浏览过
local exists = redis.call('EXISTS', dedupeKey)
if exists == 1 then
    return 0
end

-- 2. 上限检查：当前浏览量已达上限
local currentViews = tonumber(redis.call('HGET', statsKey, 'view_count') or '0')
if currentViews >= maxViews then
    return -1
end

-- 3. 原子递增浏览量
local newViews = redis.call('HINCRBY', statsKey, 'view_count', 1)

-- 4. 设置去重 key
redis.call('SETEX', dedupeKey, dedupeTTL, '1')

-- 5. 续期 stats key
redis.call('EXPIRE', statsKey, statsTTL)

return newViews
`

var viewIncrSHA string

const maxViewCount int64 = 1000000000 // 10亿

// InitViewLuaScripts 预加载浏览量 Lua 脚本到 Redis（启动时调用）
func InitViewLuaScripts() error {
	var err error
	viewIncrSHA, err = Client.ScriptLoad(ctx, viewIncrScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load view incr script: %w", err)
	}
	return nil
}

// IncrementPostViewCountWithDedup 原子递增帖子浏览量（带去重和上限检查）
// 返回值：>0=新浏览量, 0=去重跳过, -1=已达上限
func IncrementPostViewCountWithDedup(postID, userID uuid.UUID) (int64, error) {
	statsKey := GetPostStatsKey(postID)
	dedupeKey := GetPostViewDedupeKey(postID, userID)
	statsTTLSec := int64(postStatsTTL.Seconds())
	dedupeTTL := int64(300) // 5 分钟

	result, err := Client.EvalSha(ctx, viewIncrSHA,
		[]string{statsKey, dedupeKey},
		maxViewCount, dedupeTTL, statsTTLSec,
	).Int64()

	if err != nil {
		// Redis 重启后 SHA 失效，重新加载并重试
		viewIncrSHA, err = Client.ScriptLoad(ctx, viewIncrScript).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to reload view incr script: %w", err)
		}
		result, err = Client.EvalSha(ctx, viewIncrSHA,
			[]string{statsKey, dedupeKey},
			maxViewCount, dedupeTTL, statsTTLSec,
		).Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to execute view incr: %w", err)
		}
	}

	return result, nil
}

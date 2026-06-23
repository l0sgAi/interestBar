package redis

import (
	"fmt"
	"time"
)

// slidingWindowScript 滑动窗口计数限流（加权双桶，Cloudflare 算法）。
//
// 相比固定窗口，消除窗口边界 2× 突刺：当前桶计数 + 上一桶计数按时间衰减加权。
// 每桶 key 形如 rate:{prefix}:{id}:{windowStart}，TTL = 2×window，旧桶自动过期。
//
// KEYS:
//   KEYS[1] = 当前桶 key rate:{prefix}:{id}:{curStart}
//   KEYS[2] = 上一桶 key rate:{prefix}:{id}:{prevStart}
//
// ARGV:
//   ARGV[1] = limit           窗口内允许的请求数
//   ARGV[2] = window_ms       窗口时长（毫秒）
//   ARGV[3] = now_ms          当前时间（毫秒，由 Go 侧传入）
//   ARGV[4] = bucket_ttl_ms   桶 TTL（= 2×window）
//
// 返回 {allowed(1/0), remaining, retryAfterMs}
const slidingWindowScript = `
local limit    = tonumber(ARGV[1])
local window   = tonumber(ARGV[2])
local now      = tonumber(ARGV[3])
local ttl      = tonumber(ARGV[4])

local curCount  = tonumber(redis.call('GET', KEYS[1]) or '0') or 0
local prevCount = tonumber(redis.call('GET', KEYS[2]) or '0') or 0

local curStart = math.floor(now / window) * window
local elapsed  = now - curStart                  -- 进入当前窗口的毫秒数
local weight   = (window - elapsed) / window     -- 上一桶权重
local estimated= curCount + prevCount * weight

if estimated < limit then
    local n = redis.call('INCR', KEYS[1])
    redis.call('PEXPIRE', KEYS[1], ttl)
    local remaining = limit - n
    if remaining < 0 then remaining = 0 end
    return {1, remaining, 0}
else
    local retryAfter = window - elapsed           -- 当前窗口完全翻转的等待
    if retryAfter < 0 then retryAfter = 0 end
    return {0, 0, retryAfter}
end
`

var slidingWindowSHA string

// RateLimitResult 限流判定结果。
type RateLimitResult struct {
	Allowed    bool // 是否放行
	Remaining  int  // 本窗口剩余配额（≥0）
	RetryAfter int  // 被拒时建议的重试等待毫秒（放行时为 0）
}

// InitRateLimitLuaScripts 预加载滑动窗口限流脚本（启动时调用）。
func InitRateLimitLuaScripts() error {
	var err error
	slidingWindowSHA, err = Client.ScriptLoad(ctx, slidingWindowScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load sliding window script: %w", err)
	}
	return nil
}

// SlidingWindowAllow 滑动窗口计数限流（原子）。
//
//	keyPrefix: 业务前缀（如 "rl:ip:auth-register"）
//	id:        主体标识（如客户端 IP）
//	limit:     窗口内允许的请求数
//	window:    窗口时长
//
// Redis 异常时返回 error；调用方（中间件）应 fail-open 放行并告警，不阻断主流程。
func SlidingWindowAllow(keyPrefix, id string, limit int, window time.Duration) (RateLimitResult, error) {
	now := time.Now().UnixMilli()
	windowMs := window.Milliseconds()
	curStart := (now / windowMs) * windowMs
	prevStart := curStart - windowMs
	bucketTTL := windowMs * 2

	curKey := fmt.Sprintf("rate:%s:%s:%d", keyPrefix, id, curStart)
	prevKey := fmt.Sprintf("rate:%s:%s:%d", keyPrefix, id, prevStart)
	keys := []string{curKey, prevKey}
	args := []interface{}{limit, windowMs, now, bucketTTL}

	allowed, remaining, retryAfter, err := execSlidingWindow(keys, args)
	if err != nil {
		return RateLimitResult{}, err
	}
	return RateLimitResult{Allowed: allowed == 1, Remaining: remaining, RetryAfter: retryAfter}, nil
}

// execSlidingWindow 执行 EvalSha，处理 NOSCRIPT 重载重试，解析 {allowed, remaining, retryAfter}。
func execSlidingWindow(keys []string, args []interface{}) (int, int, int, error) {
	res, err := Client.EvalSha(ctx, slidingWindowSHA, keys, args...).Result()
	if err != nil {
		slidingWindowSHA, err = Client.ScriptLoad(ctx, slidingWindowScript).Result()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to reload sliding window script: %w", err)
		}
		res, err = Client.EvalSha(ctx, slidingWindowSHA, keys, args...).Result()
		if err != nil {
			return 0, 0, 0, fmt.Errorf("failed to execute sliding window: %w", err)
		}
	}

	arr, ok := res.([]interface{})
	if !ok || len(arr) < 3 {
		return 0, 0, 0, fmt.Errorf("unexpected sliding window result: %v", res)
	}
	allowed, err := toInt(arr[0])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid allowed: %w", err)
	}
	remaining, err := toInt(arr[1])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid remaining: %w", err)
	}
	retryAfter, err := toInt(arr[2])
	if err != nil {
		return 0, 0, 0, fmt.Errorf("invalid retryAfter: %w", err)
	}
	return allowed, remaining, retryAfter, nil
}

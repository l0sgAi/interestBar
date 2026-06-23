package composition

import (
	"strconv"
	"time"

	"interestBar/pkg/logger"
	"interestBar/pkg/server/storage/redis"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
	"interestBar/pkg/shared/routing"

	"go.uber.org/zap"
)

// IPRateLimitOpt IP 限流配置。
type IPRateLimitOpt struct {
	KeyPrefix string        // 业务前缀，如 "rl:ip:auth-register"
	Limit     int           // 窗口内上限
	Window    time.Duration // 窗口时长
}

// NewIPRateLimiter 返回一个 routing.HandlerFunc，按客户端 IP 做滑动窗口限流。
//
// 行为：
//   - 命中上限 → 写 Retry-After / X-RateLimit-* 头 + 429 + Abort；
//   - 放行    → 直接 return，链路继续下一个 handler；
//   - Redis 异常 → fail-open 放行（可用性优先），打告警日志。
//
// 部署在反向代理后时，依赖 hertz 可信代理配置，ClientIP() 才能取到真实来源 IP。
func NewIPRateLimiter(opt IPRateLimitOpt) routing.HandlerFunc {
	return func(c appctx.AppContext) {
		ip := c.ClientIP()
		if ip == "" {
			ip = "unknown"
		}

		res, err := redis.SlidingWindowAllow(opt.KeyPrefix, ip, opt.Limit, opt.Window)
		if err != nil {
			// fail-open：限流是可用性护栏，Redis 故障时不阻断主流程。
			if logger.Log != nil {
				logger.Log.Warn("rate limiter error, fail-open",
					zap.String("prefix", opt.KeyPrefix),
					zap.String("ip", ip),
					zap.Error(err),
				)
			}
			return
		}

		if res.Allowed {
			return
		}

		// 被拒：写限流响应头 + 429
		retrySec := res.RetryAfter / 1000
		if retrySec <= 0 {
			retrySec = 1
		}
		c.SetHeader("Retry-After", strconv.Itoa(retrySec))
		c.SetHeader("X-RateLimit-Limit", strconv.Itoa(opt.Limit))
		c.SetHeader("X-RateLimit-Remaining", "0")
		httputil.TooManyRequests(c, httputil.MsgRateLimitExceeded)
	}
}

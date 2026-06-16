package middleware

import (
	"context"
	"time"

	"interestBar/pkg/logger"

	"github.com/cloudwego/hertz/pkg/app"
	"go.uber.org/zap"
)

// Logger 访问日志中间件。
//
// 从 pkg/server/router/middleware/log.go 迁移（gin→hertz）。
// 差异：hertz 无 c.Errors 等价物，本字段省略（如需可后续接入 hlog）。
func Logger() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		start := time.Now()
		path := string(c.Request.URI().Path())
		query := string(c.Request.URI().QueryString())

		c.Next(ctx)

		cost := time.Since(start)
		logger.Log.Info(path,
			zap.Int("status", c.Response.StatusCode()),
			zap.String("method", string(c.Method())),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.String("user-agent", string(c.Request.Header.UserAgent())),
			zap.Duration("cost", cost),
		)
	}
}

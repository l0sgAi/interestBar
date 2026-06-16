// Package middleware 提供 hertz 版的全局中间件（CORS、Logger）。
//
// 中间件属于装配层（composition）的关注点，放在本目录。
// 签名采用 hertz 原生 app.HandlerFunc：
//
//	func(ctx context.Context, c *app.RequestContext)
//
// 从 pkg/server/router/middleware/ 迁移而来（gin→hertz），逻辑等价。
package middleware

import (
	"context"
	"strings"

	"interestBar/pkg/conf"

	"github.com/cloudwego/hertz/pkg/app"
)

// CORS 跨域中间件。
//
// 行为与旧 pkg/server/router/middleware/cors.go 一致：
//   - 无 Origin 头（同源请求）直接放行；
//   - Origin 命中 AllowedOrigins（支持 *、精确、:* 端口通配、路径前缀）则放行并写 CORS 头；
//   - OPTIONS 预检：命中返回 204，未命中返回 403；
//   - 其他方法命中则继续，未命中也继续（不阻断，仅不写 CORS 头）。
func CORS() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		// 从配置文件获取允许的前端地址
		allowedOrigins := conf.Config.CORS.AllowedOrigins

		// 获取请求的 Origin
		origin := string(c.GetHeader("Origin"))

		// 如果没有 Origin 头(比如同源请求),直接放行
		if origin == "" {
			c.Next(ctx)
			return
		}

		// 检查 Origin 是否在允许列表中
		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			// 支持通配符匹配
			if allowedOrigin == "*" {
				allowed = true
				break
			}
			// 精确匹配
			if allowedOrigin == origin {
				allowed = true
				break
			}
			// 支持通配符前缀匹配 (如 http://localhost:* 匹配所有 localhost 端口)
			if strings.HasSuffix(allowedOrigin, ":*") {
				prefix := strings.TrimSuffix(allowedOrigin, ":*")
				if strings.HasPrefix(origin, prefix) {
					allowed = true
					break
				}
			}
			// 支持路径前缀匹配 (如 https://example.com 匹配 https://example.com/foo)
			if strings.HasPrefix(origin, allowedOrigin+"/") {
				allowed = true
				break
			}
		}

		// 设置 CORS 头
		if allowed {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, satoken,ngrok-skip-browser-warning")
			c.Header("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
			c.Header("Access-Control-Max-Age", "86400")
		}

		// 处理预检请求
		if string(c.Method()) == "OPTIONS" {
			if allowed {
				c.AbortWithStatus(204)
			} else {
				c.AbortWithStatus(403)
			}
			return
		}

		c.Next(ctx)
	}
}

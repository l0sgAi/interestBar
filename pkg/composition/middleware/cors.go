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
		allowed := isOriginAllowed(origin, allowedOrigins)

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

// isOriginAllowed 检查 origin 是否命中允许列表。
//
// 匹配规则（与旧逻辑等价，仅修复 :* 通配的边界缺陷）：
//   - "*"：通配所有 origin；
//   - 精确匹配：allowedOrigin == origin；
//   - ":*" 端口通配：allowedOrigin 形如 "scheme://host:*"，匹配 "scheme://host:<端口>"，
//     要求端口必须是纯数字，避免 "http://localhost:*" 误匹配 "http://localhost.evil.com"
//     （旧实现用 HasPrefix(prefix) 会把后者也放行）；
//   - 路径前缀：origin == allowedOrigin + "/..."。
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, allowedOrigin := range allowedOrigins {
		// 通配符匹配所有
		if allowedOrigin == "*" {
			return true
		}
		// 精确匹配
		if allowedOrigin == origin {
			return true
		}
		// 端口通配：scheme://host:*  →  scheme://host:<digits>
		if strings.HasSuffix(allowedOrigin, ":*") {
			prefix := strings.TrimSuffix(allowedOrigin, ":*")
			// 要求 origin 形如 prefix + ":" + 纯数字端口
			if strings.HasPrefix(origin, prefix+":") {
				rest := origin[len(prefix)+1:]
				if isAllDigits(rest) {
					return true
				}
			}
			continue
		}
		// 路径前缀匹配 (如 https://example.com 匹配 https://example.com/foo)
		if strings.HasPrefix(origin, allowedOrigin+"/") {
			return true
		}
	}
	return false
}

// isAllDigits 判断 s 是否非空且全为 ASCII 数字。
// 用于 :* 端口通配时校验端口部分，防止 "localhost:*" 匹配 "localhost.evil.com"。
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

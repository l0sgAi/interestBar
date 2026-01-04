package middleware

import (
	"interestBar/pkg/conf"
	"strings"

	"github.com/gin-gonic/gin"
)

// CORS 跨域中间件
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 从配置文件获取允许的前端地址
		allowedOrigins := conf.Config.CORS.AllowedOrigins

		// 获取请求的 Origin
		origin := c.Request.Header.Get("Origin")

		// 如果没有 Origin 头(比如同源请求),直接放行
		if origin == "" {
			c.Next()
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
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With, satoken,ngrok-skip-browser-warning")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE, PATCH")
			c.Writer.Header().Set("Access-Control-Max-Age", "86400")
		}

		// 处理预检请求
		if c.Request.Method == "OPTIONS" {
			if allowed {
				c.AbortWithStatus(204)
			} else {
				c.AbortWithStatus(403)
			}
			return
		}

		c.Next()
	}
}

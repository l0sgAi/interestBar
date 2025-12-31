package middleware

import "github.com/gin-gonic/gin"

func Cache() gin.HandlerFunc {
	return func(c *gin.Context) {
		// TODO: Implement Cache
		c.Next()
	}
}

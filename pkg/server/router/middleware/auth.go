package middleware

import (
	"interestBar/pkg/server/response"
	"interestBar/pkg/util"
	"interestBar/pkg/server/storage/cache/redis"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// Auth validates JWT tokens and checks Redis
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			response.Unauthorized(c, response.MsgTokenRequired)
			c.Abort()
			return
		}

		// Check if it's a Bearer token
		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			response.Unauthorized(c, "Invalid authorization format")
			c.Abort()
			return
		}

		token := parts[1]

		// Parse JWT token
		claims, err := util.ParseToken(token)
		if err != nil {
			response.Unauthorized(c, response.MsgInvalidToken)
			c.Abort()
			return
		}

		// Check if token exists in Redis
		userID, err := redis.GetToken(token)
		if err != nil {
			response.Unauthorized(c, response.MsgSessionExpired)
			c.Abort()
			return
		}

		// Verify user ID matches
		if userID != claims.UserID {
			response.Unauthorized(c, "Token user mismatch")
			c.Abort()
			return
		}

		// Store user info in context
		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Set("token", token)

		c.Next()
	}
}

// OptionalAuth is similar to Auth but doesn't abort if no token
func OptionalAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.Next()
			return
		}

		parts := strings.SplitN(authHeader, " ", 2)
		if !(len(parts) == 2 && parts[0] == "Bearer") {
			c.Next()
			return
		}

		token := parts[1]
		claims, err := util.ParseToken(token)
		if err != nil {
			c.Next()
			return
		}

		userID, err := redis.GetToken(token)
		if err != nil || userID != claims.UserID {
			c.Next()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("email", claims.Email)
		c.Set("role", claims.Role)
		c.Set("token", token)

		c.Next()
	}
}

// RoleAuth checks if user has required role
func RoleAuth(requiredRole int) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user role from context (set by Auth)
		role, exists := c.Get("role")
		if !exists {
			response.Unauthorized(c, response.MsgLoginRequired)
			c.Abort()
			return
		}

		userRole, ok := role.(int)
		if !ok {
			response.InternalError(c, "Invalid role type")
			c.Abort()
			return
		}

		// Check if user has required role (higher number = higher privilege)
		if userRole < requiredRole {
			response.Forbidden(c, response.MsgPermissionDenied)
			c.Abort()
			return
		}

		c.Next()
	}
}

// RefreshToken extends token expiration
func RefreshToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, exists := c.Get("token")
		if !exists {
			c.Next()
			return
		}

		tokenStr, ok := token.(string)
		if !ok {
			c.Next()
			return
		}

		userID, exists := c.Get("user_id")
		if !exists {
			c.Next()
			return
		}

		// Extend token expiration by 3 days
		userIDUint, _ := userID.(uint)
		redis.SetToken(tokenStr, userIDUint, 3*24*time.Hour)

		c.Next()
	}
}

package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"interestBar/pkg/server/response"
	"interestBar/pkg/server/storage/cache/redis"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	csrfTokenLength = 32
	csrfHeaderKey   = "X-CSRF-Token"
	csrfCookieName  = "csrf_token"
	csrfQueryKey    = "csrf_token"
)

// GenerateCSRFToken generates a secure random CSRF token
func GenerateCSRFToken() (string, error) {
	b := make([]byte, csrfTokenLength)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// CSRF provides CSRF protection for state-changing operations
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip CSRF for GET, HEAD, OPTIONS, TRACE (safe methods)
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" ||
			c.Request.Method == "OPTIONS" || c.Request.Method == "TRACE" {
			c.Next()
			return
		}

		// Get CSRF token from header, query, or form
		token := c.GetHeader(csrfHeaderKey)
		if token == "" {
			token = c.Query(csrfQueryKey)
		}
		if token == "" {
			token = c.PostForm(csrfQueryKey)
		}

		if token == "" {
			response.Forbidden(c, response.MsgCSRFTokenRequired)
			c.Abort()
			return
		}

		// Get session ID from context (set by Auth middleware)
		sessionID, exists := c.Get("user_id")
		if !exists {
			response.Unauthorized(c, response.MsgLoginRequired)
			c.Abort()
			return
		}

		// Verify CSRF token in Redis
		csrfKey := buildCSRFKey(sessionID.(uint))
		storedToken, err := redis.Client.Get(redis.Ctx, csrfKey).Result()
		if err != nil || storedToken != token {
			response.Forbidden(c, response.MsgInvalidCSRFToken)
			c.Abort()
			return
		}

		c.Next()
	}
}

// SetCSRFToken generates and sets a new CSRF token for the session
func SetCSRFToken(c *gin.Context) error {
	userID, exists := c.Get("user_id")
	if !exists {
		return nil // No user, no CSRF token needed
	}

	// Generate new token
	token, err := GenerateCSRFToken()
	if err != nil {
		return err
	}

	// Store in Redis with 24 hour expiration
	csrfKey := buildCSRFKey(userID.(uint))
	err = redis.Client.Set(redis.Ctx, csrfKey, token, 24*time.Hour).Err()
	if err != nil {
		return err
	}

	// Set in response header
	c.Header(csrfHeaderKey, token)

	return nil
}

// GetCSRFToken returns the CSRF token for the current session
func GetCSRFToken(c *gin.Context) (string, error) {
	userID, exists := c.Get("user_id")
	if !exists {
		return "", nil
	}

	csrfKey := buildCSRFKey(userID.(uint))
	token, err := redis.Client.Get(redis.Ctx, csrfKey).Result()
	if err != nil {
		return "", err
	}

	return token, nil
}

// buildCSRFKey builds the Redis key for storing CSRF token
func buildCSRFKey(userID uint) string {
	return "csrf:token:" + strconv.FormatUint(uint64(userID), 10)
}

// CSRFMiddleware is a convenient middleware that sets CSRF token for GET requests
// and validates for other methods
func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// For GET requests, generate and set CSRF token
		if c.Request.Method == "GET" {
			if err := SetCSRFToken(c); err != nil {
				// Log error but don't fail the request
				// In production, you might want to log this properly
			}
			c.Next()
			return
		}

		// For other methods, validate CSRF token
		CSRF()(c)
	}
}

// ValidateCSRFOrigin validates the Origin header for CSRF protection
// This is an additional layer of defense
func ValidateCSRFOrigin(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Skip for safe methods
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" ||
			c.Request.Method == "OPTIONS" || c.Request.Method == "TRACE" {
			c.Next()
			return
		}

		// Get Origin or Referer header
		origin := c.GetHeader("Origin")
		if origin == "" {
			// Fallback to Referer
			referer := c.GetHeader("Referer")
			if referer != "" {
				// Extract origin from referer
				if idx := strings.Index(referer, "//"); idx != -1 {
					if idx2 := strings.Index(referer[idx+2:], "/"); idx2 != -1 {
						origin = referer[:idx+2+idx2]
					} else {
						origin = referer
					}
				}
			}
		}

		// Check if origin is allowed
		if origin == "" {
			response.Forbidden(c, "Origin header is required")
			c.Abort()
			return
		}

		allowed := false
		for _, allowedOrigin := range allowedOrigins {
			if origin == allowedOrigin {
				allowed = true
				break
			}
		}

		if !allowed {
			response.Forbidden(c, response.MsgOriginNotAllowed)
			c.Abort()
			return
		}

		c.Next()
	}
}

package middleware

import (
	"crypto/rand"
	"encoding/base64"
	"interestBar/pkg/server/response"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/click33/sa-token-go/stputil"
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

		// Get login ID from context (set by SaTokenAuth middleware)
		loginID, exists := c.Get("login_id")
		if !exists {
			response.Unauthorized(c, response.MsgLoginRequired)
			c.Abort()
			return
		}

		// Verify CSRF token in Sa-Token Session
		session, err := stputil.GetSession(loginID)
		if err != nil {
			response.InternalError(c, "Failed to get session")
			c.Abort()
			return
		}

		storedToken, exists := session.Get("csrf_token")
		if !exists || storedToken != token {
			response.Forbidden(c, response.MsgInvalidCSRFToken)
			c.Abort()
			return
		}

		c.Next()
	}
}

// SetCSRFToken generates and sets a new CSRF token for the session
func SetCSRFToken(c *gin.Context) error {
	loginID, exists := c.Get("login_id")
	if !exists {
		return nil // No user, no CSRF token needed
	}

	// Generate new token
	token, err := GenerateCSRFToken()
	if err != nil {
		return err
	}

	// Store in Sa-Token Session
	session, err := stputil.GetSession(loginID)
	if err != nil {
		return err
	}

	session.Set("csrf_token", token)
	session.Set("csrf_token_expires", time.Now().Add(24*time.Hour).Unix())

	// Set in response header
	c.Header(csrfHeaderKey, token)

	return nil
}

// GetCSRFToken returns the CSRF token for the current session
func GetCSRFToken(c *gin.Context) (string, error) {
	loginID, exists := c.Get("login_id")
	if !exists {
		return "", nil
	}

	session, err := stputil.GetSession(loginID)
	if err != nil {
		return "", err
	}

	token, exists := session.Get("csrf_token")
	if !exists {
		return "", nil
	}

	return token.(string), nil
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

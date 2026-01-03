package util

import (
	"interestBar/pkg/conf"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// TokenExpiration defines the default token expiration time (3 days)
	TokenExpiration = 3 * 24 * time.Hour
)

// getJwtSecret retrieves JWT secret from config
func getJwtSecret() []byte {
	if conf.Config != nil && conf.Config.JwtSecret != "" {
		return []byte(conf.Config.JwtSecret)
	}
	// Fallback for development - should never happen in production
	return []byte("please_set_jwt_secret_in_config")
}

type Claims struct {
	UserID uint   `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
	Role   int    `json:"role,omitempty"`
	// For Binding Token
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken generates a standard auth token with 3 days expiration
func GenerateToken(userID uint, email string, role int) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(TokenExpiration)),
			Issuer:    "interestBar",
		},
	}
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tokenClaims.SignedString(getJwtSecret())
}

// GenerateBindingToken generates a temporary token for registration binding
func GenerateBindingToken(provider, providerID, email string) (string, error) {
	claims := Claims{
		Email:      email,
		Provider:   provider,
		ProviderID: providerID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)), // Short lived
			Issuer:    "interestBar",
			Subject:   "binding",
		},
	}
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tokenClaims.SignedString(getJwtSecret())
}

// ParseToken parses the token
func ParseToken(token string) (*Claims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return getJwtSecret(), nil
	})

	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*Claims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}
	return nil, err
}

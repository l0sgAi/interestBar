package util

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var JwtSecret = []byte("your_jwt_secret") // Should be loaded from config

type Claims struct {
	UserID uint   `json:"user_id,omitempty"`
	Email  string `json:"email,omitempty"`
	Role   int    `json:"role,omitempty"`
	// For Binding Token
	Provider   string `json:"provider,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	jwt.RegisteredClaims
}

// GenerateToken generates a standard auth token
func GenerateToken(userID uint, email string, role int) (string, error) {
	claims := Claims{
		UserID: userID,
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			Issuer:    "interestBar",
		},
	}
	tokenClaims := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return tokenClaims.SignedString(JwtSecret)
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
	return tokenClaims.SignedString(JwtSecret)
}

// ParseToken parses the token
func ParseToken(token string) (*Claims, error) {
	tokenClaims, err := jwt.ParseWithClaims(token, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return JwtSecret, nil
	})

	if tokenClaims != nil {
		if claims, ok := tokenClaims.Claims.(*Claims); ok && tokenClaims.Valid {
			return claims, nil
		}
	}
	return nil, err
}

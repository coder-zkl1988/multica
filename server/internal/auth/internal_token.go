package auth

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type InternalTokenIdentity struct {
	UserID    string
	Email     string
	Source    string
	ExpiresAt time.Time
}

func ParseInternalToken(raw string) (InternalTokenIdentity, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
		}
		return JWTSecret(), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return InternalTokenIdentity{}, fmt.Errorf("invalid internal token: %w", err)
	}
	userID, err := claims.GetSubject()
	if err != nil || strings.TrimSpace(userID) == "" {
		return InternalTokenIdentity{}, errors.New("invalid internal token subject")
	}
	source, _ := claims["auth_source"].(string)
	if source != "sso" {
		return InternalTokenIdentity{}, errors.New("invalid internal token auth source")
	}
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return InternalTokenIdentity{}, errors.New("invalid internal token expiry")
	}
	email, _ := claims["email"].(string)
	return InternalTokenIdentity{UserID: userID, Email: email, Source: source, ExpiresAt: expiresAt.Time}, nil
}

func ParseLegacyJWT(raw string) (InternalTokenIdentity, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method %q", token.Method.Alg())
		}
		return JWTSecret(), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return InternalTokenIdentity{}, fmt.Errorf("invalid legacy token: %w", err)
	}
	userID, err := claims.GetSubject()
	if err != nil || strings.TrimSpace(userID) == "" {
		return InternalTokenIdentity{}, errors.New("invalid legacy token subject")
	}
	if value, exists := claims["auth_source"]; exists && value != nil {
		source, ok := value.(string)
		if !ok || strings.TrimSpace(source) != "" {
			return InternalTokenIdentity{}, errors.New("invalid legacy token auth source")
		}
	}
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return InternalTokenIdentity{}, errors.New("invalid legacy token expiry")
	}
	email, _ := claims["email"].(string)
	return InternalTokenIdentity{UserID: userID, Email: email, ExpiresAt: expiresAt.Time}, nil
}

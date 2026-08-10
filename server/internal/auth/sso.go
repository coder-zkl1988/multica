package auth

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net/mail"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const SSOCookieName = "sy_sso_token"

type SSOIdentity struct {
	Email       string
	DisplayName string
	ExpiresAt   time.Time
}

type SSOVerifier struct {
	publicKey   *rsa.PublicKey
	expectedSub string
}

func NewSSOVerifier(publicKeyPEM []byte, expectedSub string) (*SSOVerifier, error) {
	expectedSub = strings.TrimSpace(expectedSub)
	if expectedSub == "" {
		return nil, errors.New("SSO expected subject is required")
	}
	block, _ := pem.Decode(publicKeyPEM)
	if block == nil {
		return nil, errors.New("SSO public key is not PEM")
	}
	publicKey, err := parseRSAPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse SSO public key: %w", err)
	}
	return &SSOVerifier{publicKey: publicKey, expectedSub: expectedSub}, nil
}

func parseRSAPublicKey(der []byte) (*rsa.PublicKey, error) {
	if key, err := x509.ParsePKIXPublicKey(der); err == nil {
		if rsaKey, ok := key.(*rsa.PublicKey); ok {
			return rsaKey, nil
		}
		return nil, errors.New("public key is not RSA")
	}
	return x509.ParsePKCS1PublicKey(der)
}

func LoadSSOVerifierFromEnv() (*SSOVerifier, error) {
	path := strings.TrimSpace(os.Getenv("SSO_PUBLIC_KEY_PATH"))
	if path == "" {
		return nil, errors.New("SSO_PUBLIC_KEY_PATH is required when USE_SY_SSO=true")
	}
	publicKeyPEM, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read SSO public key: %w", err)
	}
	return NewSSOVerifier(publicKeyPEM, os.Getenv("SSO_EXPECTED_SUB"))
}

func LoadUseSySSOFromEnv() (bool, error) {
	raw := strings.TrimSpace(os.Getenv("USE_SY_SSO"))
	switch strings.ToLower(raw) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("USE_SY_SSO must be true or false, got %q", raw)
	}
}

func LoadDevAuthEmailFromEnv() (string, error) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("MULTICA_DEV_AUTH_EMAIL")))
	if email == "" {
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV"))) {
	case "development", "dev", "local":
	default:
		return "", errors.New("MULTICA_DEV_AUTH_EMAIL requires APP_ENV=development, dev, or local")
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return "", errors.New("MULTICA_DEV_AUTH_EMAIL must be an email address")
	}
	return email, nil
}

func (v *SSOVerifier) Verify(raw string) (SSOIdentity, error) {
	if v == nil {
		return SSOIdentity{}, errors.New("SSO is not configured")
	}
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("unexpected SSO signing method %q", token.Method.Alg())
		}
		return v.publicKey, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}), jwt.WithExpirationRequired())
	if err != nil || !token.Valid {
		return SSOIdentity{}, fmt.Errorf("invalid SSO token: %w", err)
	}

	subject, err := claims.GetSubject()
	if err != nil || subject != v.expectedSub {
		return SSOIdentity{}, errors.New("invalid SSO subject")
	}
	expiresAt, err := claims.GetExpirationTime()
	if err != nil || expiresAt == nil {
		return SSOIdentity{}, errors.New("invalid SSO expiry")
	}
	data, ok := claims["data"].(map[string]any)
	if !ok {
		return SSOIdentity{}, errors.New("invalid SSO identity data")
	}
	email := strings.ToLower(strings.TrimSpace(stringClaim(data, "mail")))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		return SSOIdentity{}, errors.New("invalid SSO email")
	}
	displayName := strings.TrimSpace(stringClaim(data, "display"))
	if displayName == "" {
		return SSOIdentity{}, errors.New("invalid SSO display name")
	}
	return SSOIdentity{Email: email, DisplayName: displayName, ExpiresAt: expiresAt.Time}, nil
}

func stringClaim(claims map[string]any, name string) string {
	value, _ := claims[name].(string)
	return value
}

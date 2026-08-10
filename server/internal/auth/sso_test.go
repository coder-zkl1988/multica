package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestSSOVerifier(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey),
	})
	verifier, err := NewSSOVerifier(publicPEM, "multica")
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)

	sign := func(method jwt.SigningMethod, claims jwt.MapClaims, key any) string {
		t.Helper()
		raw, err := jwt.NewWithClaims(method, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	validClaims := func() jwt.MapClaims {
		return jwt.MapClaims{
			"sub": "multica",
			"exp": expiresAt.Unix(),
			"data": map[string]any{
				"mail":    " Employee@Soyoung.com ",
				"display": "Employee Name",
			},
		}
	}

	identity, err := verifier.Verify(sign(jwt.SigningMethodRS256, validClaims(), privateKey))
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if identity.Email != "employee@soyoung.com" || identity.DisplayName != "Employee Name" || !identity.ExpiresAt.Equal(expiresAt) {
		t.Fatalf("Verify() = %#v", identity)
	}

	tests := []struct {
		name   string
		method jwt.SigningMethod
		claims jwt.MapClaims
		key    any
	}{
		{"rejects HS256", jwt.SigningMethodHS256, validClaims(), []byte("secret")},
		{"requires exp", jwt.SigningMethodRS256, func() jwt.MapClaims { c := validClaims(); delete(c, "exp"); return c }(), privateKey},
		{"rejects expired exp", jwt.SigningMethodRS256, func() jwt.MapClaims { c := validClaims(); c["exp"] = time.Now().Add(-time.Minute).Unix(); return c }(), privateKey},
		{"requires exact sub", jwt.SigningMethodRS256, func() jwt.MapClaims { c := validClaims(); c["sub"] = "other"; return c }(), privateKey},
		{"requires valid email", jwt.SigningMethodRS256, func() jwt.MapClaims {
			c := validClaims()
			c["data"] = map[string]any{"mail": "not-an-email", "display": "Employee"}
			return c
		}(), privateKey},
		{"requires display", jwt.SigningMethodRS256, func() jwt.MapClaims {
			c := validClaims()
			c["data"] = map[string]any{"mail": "employee@soyoung.com", "display": " "}
			return c
		}(), privateKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := verifier.Verify(sign(tc.method, tc.claims, tc.key)); err == nil {
				t.Fatal("Verify() error = nil")
			}
		})
	}
}

func TestLoadDevAuthEmailFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		appEnv  string
		email   string
		want    string
		wantErr bool
	}{
		{name: "unset", appEnv: "production"},
		{name: "development", appEnv: "development", email: " Dev@Example.com ", want: "dev@example.com"},
		{name: "dev", appEnv: "dev", email: "dev@example.com", want: "dev@example.com"},
		{name: "local", appEnv: "local", email: "dev@example.com", want: "dev@example.com"},
		{name: "invalid email", appEnv: "development", email: "not-an-email", wantErr: true},
		{name: "production", appEnv: "production", email: "dev@example.com", wantErr: true},
		{name: "staging", appEnv: "staging", email: "dev@example.com", wantErr: true},
		{name: "empty environment", email: "dev@example.com", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.appEnv)
			t.Setenv("MULTICA_DEV_AUTH_EMAIL", tc.email)
			got, err := LoadDevAuthEmailFromEnv()
			if (err != nil) != tc.wantErr {
				t.Fatalf("LoadDevAuthEmailFromEnv() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("LoadDevAuthEmailFromEnv() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLoadUseSySSOFromEnv(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    bool
		wantErr bool
	}{
		{name: "unset"},
		{name: "false", value: "false"},
		{name: "false uppercase", value: " FALSE "},
		{name: "true", value: "true", want: true},
		{name: "true mixed case", value: " TrUe ", want: true},
		{name: "rejects ambiguous value", value: "1", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("USE_SY_SSO", tc.value)
			got, err := LoadUseSySSOFromEnv()
			if (err != nil) != tc.wantErr {
				t.Fatalf("LoadUseSySSOFromEnv() error = %v, wantErr %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("LoadUseSySSOFromEnv() = %v, want %v", got, tc.want)
			}
		})
	}
}

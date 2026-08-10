package middleware

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
)

// testSigner sets up env vars and creates a CloudFrontSigner with a throwaway RSA key.
func testSigner(t *testing.T) *auth.CloudFrontSigner {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pkcs8Bytes, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemBlock := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8Bytes})
	b64Key := base64.StdEncoding.EncodeToString(pemBlock)

	t.Setenv("CLOUDFRONT_KEY_PAIR_ID", "TESTKEY")
	t.Setenv("CLOUDFRONT_DOMAIN", "cdn.example.com")
	t.Setenv("COOKIE_DOMAIN", ".example.com")
	t.Setenv("CLOUDFRONT_PRIVATE_KEY_SECRET", "")
	t.Setenv("CLOUDFRONT_PRIVATE_KEY", b64Key)

	signer := auth.NewCloudFrontSignerFromEnv()
	if signer == nil {
		t.Fatal("failed to create test CloudFrontSigner")
	}
	return signer
}

func TestRefreshCloudFrontCookies_UsesCurrentAuthExpiry(t *testing.T) {
	signer := testSigner(t)
	handler := RefreshCloudFrontCookies(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Auth-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("expected CloudFront cookies to be set")
	}

	for _, c := range cookies {
		untilExpiry := time.Until(c.Expires)
		if untilExpiry < 50*time.Minute || untilExpiry > 70*time.Minute {
			t.Errorf("cookie %q expires in %v; expected current auth expiry (~1h)", c.Name, untilExpiry)
		}
	}
}

func TestRefreshCloudFrontCookies_SkipsWithoutAuthExpiry(t *testing.T) {
	signer := testSigner(t)
	handler := RefreshCloudFrontCookies(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("must not mint CDN cookies without an authoritative auth expiry")
	}
}

func TestRefreshCloudFrontCookies_NilSigner(t *testing.T) {
	handler := RefreshCloudFrontCookies(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Auth-Expires-At", time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if len(rec.Result().Cookies()) != 0 {
		t.Error("nil signer should not set any cookies")
	}
}

func TestRefreshCloudFrontCookies_SkipsWhenCookiePresent(t *testing.T) {
	signer := testSigner(t)
	handler := RefreshCloudFrontCookies(signer)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "CloudFront-Policy", Value: "existing"})
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if len(rec.Result().Cookies()) != 0 {
		t.Error("should not refresh cookies when CloudFront-Policy is already present")
	}
}

package handler

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/multica-ai/multica/server/internal/auth"
)

func TestSSOSession(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)})
	verifier, err := auth.NewSSOVerifier(publicPEM, "multica")
	if err != nil {
		t.Fatal(err)
	}
	previousVerifier := testHandler.SSOVerifier
	testHandler.SSOVerifier = verifier
	t.Cleanup(func() { testHandler.SSOVerifier = previousVerifier })

	missing := httptest.NewRecorder()
	testHandler.SSOSession(missing, httptest.NewRequest(http.MethodPost, "/auth/sso/session", nil))
	if missing.Code != http.StatusUnauthorized {
		t.Fatalf("missing cookie status = %d, want %d", missing.Code, http.StatusUnauthorized)
	}

	email := fmt.Sprintf("sso-%d@soyoung.com", time.Now().UnixNano())
	expiresAt := time.Now().Add(time.Hour).Truncate(time.Second)
	raw, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":  "multica",
		"exp":  expiresAt.Unix(),
		"data": map[string]any{"mail": email, "display": "SSO Employee"},
	}).SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	request := func() *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/auth/sso/session", nil)
		req.AddCookie(&http.Cookie{Name: auth.SSOCookieName, Value: raw})
		rec := httptest.NewRecorder()
		testHandler.SSOSession(rec, req)
		return rec
	}
	first := request()
	if first.Code != http.StatusOK {
		t.Fatalf("first session status = %d, body = %s", first.Code, first.Body.String())
	}
	var firstBody map[string]json.RawMessage
	if err := json.Unmarshal(first.Body.Bytes(), &firstBody); err != nil {
		t.Fatal(err)
	}
	if _, exists := firstBody["token"]; exists {
		t.Fatal("web SSO response exposed a bearer token")
	}
	var firstUser UserResponse
	if err := json.Unmarshal(firstBody["user"], &firstUser); err != nil {
		t.Fatal(err)
	}
	if firstUser.Email != email || firstUser.Name != "SSO Employee" {
		t.Fatalf("created user = %#v", firstUser)
	}
	var memberships int
	if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM member WHERE user_id = $1`, parseUUID(firstUser.ID)).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 {
		t.Fatalf("new SSO user memberships = %d, want 0", memberships)
	}
	for _, cookie := range first.Result().Cookies() {
		if !cookie.Expires.Equal(expiresAt) {
			t.Fatalf("cookie %q expiry = %v, want %v", cookie.Name, cookie.Expires, expiresAt)
		}
	}

	second := request()
	if second.Code != http.StatusOK {
		t.Fatalf("second session status = %d, body = %s", second.Code, second.Body.String())
	}
	var secondBody struct {
		User UserResponse `json:"user"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondBody); err != nil {
		t.Fatal(err)
	}
	if secondBody.User.ID != firstUser.ID {
		t.Fatalf("second session user ID = %s, want %s", secondBody.User.ID, firstUser.ID)
	}
}

func TestSSOSessionDevBypass(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	email := fmt.Sprintf("dev-sso-%d@example.com", time.Now().UnixNano())
	previousConfig := testHandler.cfg
	testHandler.cfg.DevAuthEmail = email
	t.Cleanup(func() { testHandler.cfg = previousConfig })

	startedAt := time.Now()
	rec := httptest.NewRecorder()
	testHandler.SSOSession(rec, httptest.NewRequest(http.MethodPost, "/auth/sso/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev session status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if _, exists := body["token"]; exists {
		t.Fatal("dev SSO response exposed a bearer token")
	}
	var user UserResponse
	if err := json.Unmarshal(body["user"], &user); err != nil {
		t.Fatal(err)
	}
	if user.Email != email || user.Name != strings.SplitN(email, "@", 2)[0] {
		t.Fatalf("dev SSO user = %#v", user)
	}
	foundCookies := 0
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name != auth.AuthCookieName && cookie.Name != auth.CSRFCookieName {
			continue
		}
		foundCookies++
		if cookie.Expires.Before(startedAt.Add(8*time.Hour-time.Second)) || cookie.Expires.After(time.Now().Add(8*time.Hour+time.Second)) {
			t.Fatalf("cookie %q expiry = %v, want approximately eight hours", cookie.Name, cookie.Expires)
		}
	}
	if foundCookies != 2 {
		t.Fatalf("auth cookies = %d, want 2", foundCookies)
	}

	invalid := httptest.NewRequest(http.MethodPost, "/auth/sso/session", nil)
	invalid.AddCookie(&http.Cookie{Name: auth.SSOCookieName, Value: "not-a-token"})
	invalidRec := httptest.NewRecorder()
	testHandler.SSOSession(invalidRec, invalid)
	if invalidRec.Code != http.StatusUnauthorized {
		t.Fatalf("invalid real cookie status = %d, want %d", invalidRec.Code, http.StatusUnauthorized)
	}
}

func TestSSOPKCEAuthorizationCode(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PUBLIC KEY", Bytes: x509.MarshalPKCS1PublicKey(&privateKey.PublicKey)})
	verifier, err := auth.NewSSOVerifier(publicPEM, "multica")
	if err != nil {
		t.Fatal(err)
	}
	previousVerifier := testHandler.SSOVerifier
	testHandler.SSOVerifier = verifier
	t.Cleanup(func() { testHandler.SSOVerifier = previousVerifier })

	email := fmt.Sprintf("pkce-%d@soyoung.com", time.Now().UnixNano())
	ssoExpiresAt := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	ssoToken, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"sub":  "multica",
		"exp":  ssoExpiresAt.Unix(),
		"data": map[string]any{"mail": email, "display": "PKCE Employee"},
	}).SignedString(privateKey)
	if err != nil {
		t.Fatal(err)
	}

	codeVerifier := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFG"
	challengeBytes := sha256.Sum256([]byte(codeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(challengeBytes[:])
	redirectURI := "http://127.0.0.1:43123/callback"
	authorizeURL := "/auth/sso/authorize?" + url.Values{
		"client_id":             {"cli"},
		"redirect_uri":          {redirectURI},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {"opaque-state"},
	}.Encode()
	authorizeReq := httptest.NewRequest(http.MethodGet, authorizeURL, nil)
	authorizeReq.AddCookie(&http.Cookie{Name: auth.SSOCookieName, Value: ssoToken})
	authorizeRec := httptest.NewRecorder()
	testHandler.SSOAuthorize(authorizeRec, authorizeReq)
	if authorizeRec.Code != http.StatusFound {
		t.Fatalf("authorize status = %d, body = %s", authorizeRec.Code, authorizeRec.Body.String())
	}
	location, err := url.Parse(authorizeRec.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	code := location.Query().Get("code")
	if code == "" || location.Query().Get("state") != "opaque-state" {
		t.Fatalf("authorize redirect = %s", location.String())
	}
	if location.Query().Has("token") {
		t.Fatal("authorize redirect exposed a token")
	}

	exchange := func(verifier string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]string{
			"grant_type":    "authorization_code",
			"client_id":     "cli",
			"redirect_uri":  redirectURI,
			"code":          code,
			"code_verifier": verifier,
		})
		req := httptest.NewRequest(http.MethodPost, "/auth/sso/token", bytes.NewReader(body))
		rec := httptest.NewRecorder()
		testHandler.SSOToken(rec, req)
		return rec
	}
	wrong := exchange("abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG")
	if wrong.Code != http.StatusBadRequest {
		t.Fatalf("wrong verifier status = %d, want 400", wrong.Code)
	}
	first := exchange(codeVerifier)
	if first.Code != http.StatusOK {
		t.Fatalf("exchange status = %d, body = %s", first.Code, first.Body.String())
	}
	var tokenBody struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &tokenBody); err != nil {
		t.Fatal(err)
	}
	if tokenBody.Token == "" || !tokenBody.ExpiresAt.Equal(ssoExpiresAt) {
		t.Fatalf("token response = %#v", tokenBody)
	}
	parsed, _, err := jwt.NewParser().ParseUnverified(tokenBody.Token, jwt.MapClaims{})
	if err != nil {
		t.Fatal(err)
	}
	jwtExpiry, err := parsed.Claims.GetExpirationTime()
	if err != nil || jwtExpiry == nil || !jwtExpiry.Time.Equal(ssoExpiresAt) {
		t.Fatalf("JWT expiry = %v, %v", jwtExpiry, err)
	}
	replay := exchange(codeVerifier)
	if replay.Code != http.StatusBadRequest {
		t.Fatalf("replay status = %d, want 400", replay.Code)
	}
}

func TestValidateSSORedirect(t *testing.T) {
	if err := validateSSORedirect("cli", "http://localhost:49152/callback", "", ""); err != nil {
		t.Fatalf("valid CLI redirect: %v", err)
	}
	if err := validateSSORedirect("cli", "http://192.168.1.5:49152/callback", "", ""); err == nil {
		t.Fatal("LAN CLI redirect accepted")
	}
	if err := validateSSORedirect("desktop", "multica://auth/callback", "multica://auth/callback", ""); err != nil {
		t.Fatalf("valid desktop redirect: %v", err)
	}
	if err := validateSSORedirect("mobile", "multica-mobile://auth/callback", "", "multica-mobile://auth/callback"); err != nil {
		t.Fatalf("valid mobile redirect: %v", err)
	}
}

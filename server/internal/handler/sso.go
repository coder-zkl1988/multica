package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (h *Handler) SSOSession(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(auth.SSOCookieName)
	var identity auth.SSOIdentity
	if err == nil && cookie.Value != "" {
		identity, err = h.SSOVerifier.Verify(cookie.Value)
		if err != nil {
			slog.Warn("SSO session rejected", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusUnauthorized, "invalid SSO session")
			return
		}
	} else if h.cfg.DevAuthEmail != "" {
		identity = auth.SSOIdentity{
			Email:       h.cfg.DevAuthEmail,
			DisplayName: strings.SplitN(h.cfg.DevAuthEmail, "@", 2)[0],
			ExpiresAt:   time.Now().Add(8 * time.Hour).Truncate(time.Second),
		}
	} else {
		writeError(w, http.StatusUnauthorized, "SSO login required")
		return
	}
	user, isNew, err := h.findOrCreateSSOUser(r.Context(), identity)
	if err != nil {
		slog.Error("SSO user provisioning failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}
	if isNew {
		event := analytics.Signup(uuidToString(user.ID), user.Email, signupSourceFromRequest(r))
		event.Properties["auth_method"] = "sso"
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, event)
	}
	token, err := h.issueJWTUntil(user, identity.ExpiresAt, "sso")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	if err := auth.SetAuthCookiesUntil(w, token, identity.ExpiresAt); err != nil {
		writeError(w, http.StatusUnauthorized, "SSO session expired")
		return
	}
	if h.CFSigner != nil {
		for _, cookie := range h.CFSigner.SignedCookies(identity.ExpiresAt) {
			http.SetCookie(w, cookie)
		}
	}
	slog.Info("SSO session created", append(logger.RequestAttrs(r), "user_id", uuidToString(user.ID), "new_user", isNew)...)
	writeJSON(w, http.StatusOK, map[string]UserResponse{"user": h.userToResponse(user)})
}

func (h *Handler) findOrCreateSSOUser(ctx context.Context, identity auth.SSOIdentity) (db.User, bool, error) {
	user, err := h.Queries.GetUserByEmail(ctx, identity.Email)
	if err == nil {
		if user.AccountKind != "human" {
			return db.User{}, false, errors.New("SSO email belongs to a service account")
		}
		return user, false, nil
	}
	if !isNotFound(err) {
		return db.User{}, false, err
	}
	user, err = h.Queries.CreateUser(ctx, db.CreateUserParams{Name: identity.DisplayName, Email: identity.Email})
	if err == nil {
		return user, true, nil
	}
	// A concurrent first login may have won the unique-email insert.
	user, lookupErr := h.Queries.GetUserByEmail(ctx, identity.Email)
	if lookupErr == nil {
		return user, false, nil
	}
	return db.User{}, false, err
}

func (h *Handler) SSOAuthorize(w http.ResponseWriter, r *http.Request) {
	clientID := r.URL.Query().Get("client_id")
	redirectURI := r.URL.Query().Get("redirect_uri")
	challenge := r.URL.Query().Get("code_challenge")
	state := r.URL.Query().Get("state")
	if r.URL.Query().Get("code_challenge_method") != "S256" || !validPKCEChallenge(challenge) || state == "" || len(state) > 512 {
		writeError(w, http.StatusBadRequest, "invalid authorization request")
		return
	}
	if err := validateSSORedirect(clientID, redirectURI, h.cfg.SSODesktopRedirectURI, h.cfg.SSOMobileRedirectURI); err != nil {
		writeError(w, http.StatusBadRequest, "invalid redirect URI")
		return
	}
	cookie, err := r.Cookie(auth.SSOCookieName)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "SSO login required")
		return
	}
	identity, err := h.SSOVerifier.Verify(cookie.Value)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid SSO session")
		return
	}
	user, _, err := h.findOrCreateSSOUser(r.Context(), identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}
	codeBytes := make([]byte, 32)
	if _, err := rand.Read(codeBytes); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create authorization code")
		return
	}
	code := base64.RawURLEncoding.EncodeToString(codeBytes)
	codeHash := sha256.Sum256([]byte(code))
	now := time.Now()
	if err := h.Queries.CreateSSOAuthorizationCode(r.Context(), db.CreateSSOAuthorizationCodeParams{
		CodeHash:         codeHash[:],
		UserID:           user.ID,
		ClientID:         clientID,
		RedirectUri:      redirectURI,
		CodeChallenge:    challenge,
		SessionExpiresAt: pgtype.Timestamptz{Time: identity.ExpiresAt, Valid: true},
		ExpiresAt:        pgtype.Timestamptz{Time: now.Add(time.Minute), Valid: true},
	}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create authorization code")
		return
	}
	redirect, _ := url.Parse(redirectURI)
	query := redirect.Query()
	query.Set("code", code)
	query.Set("state", state)
	redirect.RawQuery = query.Encode()
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}

type ssoTokenRequest struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	Code         string `json:"code"`
	CodeVerifier string `json:"code_verifier"`
}

func (h *Handler) SSOToken(w http.ResponseWriter, r *http.Request) {
	var request ssoTokenRequest
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.GrantType != "authorization_code" || !validPKCEVerifier(request.CodeVerifier) {
		writeError(w, http.StatusBadRequest, "invalid token request")
		return
	}
	if err := validateSSORedirect(request.ClientID, request.RedirectURI, h.cfg.SSODesktopRedirectURI, h.cfg.SSOMobileRedirectURI); err != nil {
		writeError(w, http.StatusBadRequest, "invalid token request")
		return
	}
	verifierHash := sha256.Sum256([]byte(request.CodeVerifier))
	challenge := base64.RawURLEncoding.EncodeToString(verifierHash[:])
	codeHash := sha256.Sum256([]byte(request.Code))
	consumed, err := h.Queries.ConsumeSSOAuthorizationCode(r.Context(), db.ConsumeSSOAuthorizationCodeParams{
		CodeHash:      codeHash[:],
		ClientID:      request.ClientID,
		RedirectUri:   request.RedirectURI,
		CodeChallenge: challenge,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid or expired authorization code")
		return
	}
	user, err := h.Queries.GetUser(r.Context(), consumed.UserID)
	if err != nil || user.AccountKind != "human" {
		writeError(w, http.StatusBadRequest, "invalid authorization code")
		return
	}
	token, err := h.issueJWTUntil(user, consumed.SessionExpiresAt.Time, "sso")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Token     string       `json:"token"`
		User      UserResponse `json:"user"`
		ExpiresAt time.Time    `json:"expires_at"`
	}{Token: token, User: h.userToResponse(user), ExpiresAt: consumed.SessionExpiresAt.Time})
}

func validateSSORedirect(clientID, raw, desktop, mobile string) error {
	switch clientID {
	case "desktop":
		if desktop != "" && raw == desktop {
			return nil
		}
	case "mobile":
		if mobile != "" && raw == mobile {
			return nil
		}
	case "cli":
		redirect, err := url.Parse(raw)
		if err != nil || redirect.Scheme != "http" || (redirect.Hostname() != "localhost" && redirect.Hostname() != "127.0.0.1") || redirect.Path != "/callback" || redirect.RawQuery != "" || redirect.Fragment != "" || redirect.User != nil {
			break
		}
		port, err := strconv.Atoi(redirect.Port())
		if err == nil && port > 0 && port <= 65535 {
			return nil
		}
	}
	return errors.New("redirect URI is not allowed")
}

func validPKCEChallenge(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func validPKCEVerifier(value string) bool {
	if len(value) < 43 || len(value) > 128 {
		return false
	}
	return strings.IndexFunc(value, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("-._~", r))
	}) == -1
}

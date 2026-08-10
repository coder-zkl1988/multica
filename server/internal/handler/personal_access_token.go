package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const PATRenewThreshold = 7 * 24 * time.Hour
const PATRenewExtension = 90 * 24 * time.Hour

type PersonalAccessTokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"token_prefix"`
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

type CreatePATResponse struct {
	PersonalAccessTokenResponse
	Token string `json:"token"`
}

func patToResponse(pat db.PersonalAccessToken) PersonalAccessTokenResponse {
	return PersonalAccessTokenResponse{
		ID:         uuidToString(pat.ID),
		Name:       pat.Name,
		Prefix:     pat.TokenPrefix,
		ExpiresAt:  timestampToPtr(pat.ExpiresAt),
		LastUsedAt: timestampToPtr(pat.LastUsedAt),
		CreatedAt:  timestampToString(pat.CreatedAt),
	}
}

type CreatePATRequest struct {
	Name          string `json:"name"`
	ExpiresInDays *int   `json:"expires_in_days"`
}

func (h *Handler) CreatePersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req CreatePATRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	rawToken, err := auth.GeneratePATToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}
	var expiresAt pgtype.Timestamptz
	if req.ExpiresInDays != nil && *req.ExpiresInDays > 0 {
		expiresAt = pgtype.Timestamptz{Time: time.Now().Add(time.Duration(*req.ExpiresInDays) * 24 * time.Hour), Valid: true}
	}
	prefix := rawToken
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := h.Queries.CreatePersonalAccessToken(r.Context(), db.CreatePersonalAccessTokenParams{
		UserID: parseUUID(userID), Name: req.Name, TokenHash: auth.HashToken(rawToken), TokenPrefix: prefix, ExpiresAt: expiresAt,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}
	writeJSON(w, http.StatusCreated, CreatePATResponse{PersonalAccessTokenResponse: patToResponse(pat), Token: rawToken})
}

func (h *Handler) ListPersonalAccessTokens(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	pats, err := h.Queries.ListPersonalAccessTokensByUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list tokens")
		return
	}
	response := make([]PersonalAccessTokenResponse, len(pats))
	for i, pat := range pats {
		response[i] = patToResponse(pat)
	}
	writeJSON(w, http.StatusOK, response)
}

type RenewPATResponse struct {
	ExpiresAt string `json:"expires_at"`
	Renewed   bool   `json:"renewed"`
}

func (h *Handler) RenewCurrentPersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	authHeader := r.Header.Get("Authorization")
	rawToken := strings.TrimPrefix(authHeader, "Bearer ")
	if rawToken == "" || rawToken == authHeader || !strings.HasPrefix(rawToken, "mul_") {
		writeError(w, http.StatusBadRequest, "only personal access tokens can be renewed")
		return
	}
	hash := auth.HashToken(rawToken)
	pat, err := h.Queries.GetPersonalAccessTokenByHash(r.Context(), hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "token is no longer valid")
		} else {
			writeError(w, http.StatusInternalServerError, "failed to look up token")
		}
		return
	}
	if uuidToString(pat.UserID) != userID {
		writeError(w, http.StatusUnauthorized, "token does not belong to caller")
		return
	}
	if !pat.ExpiresAt.Valid {
		writeJSON(w, http.StatusOK, RenewPATResponse{})
		return
	}
	now := time.Now()
	if pat.ExpiresAt.Time.Sub(now) > PATRenewThreshold {
		writeJSON(w, http.StatusOK, RenewPATResponse{ExpiresAt: timestampToString(pat.ExpiresAt)})
		return
	}
	updated, err := h.Queries.ExtendPersonalAccessTokenExpiry(r.Context(), db.ExtendPersonalAccessTokenExpiryParams{
		ID:               pat.ID,
		NewExpiresAt:     pgtype.Timestamptz{Time: now.Add(PATRenewExtension), Valid: true},
		RenewThresholdAt: pgtype.Timestamptz{Time: now.Add(PATRenewThreshold), Valid: true},
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, RenewPATResponse{ExpiresAt: timestampToString(updated), Renewed: true})
	case errors.Is(err, pgx.ErrNoRows):
		current, getErr := h.Queries.GetPersonalAccessTokenByHash(r.Context(), hash)
		if getErr != nil {
			writeError(w, http.StatusUnauthorized, "token is no longer valid")
			return
		}
		writeJSON(w, http.StatusOK, RenewPATResponse{ExpiresAt: timestampToString(current.ExpiresAt)})
	default:
		writeError(w, http.StatusInternalServerError, "failed to renew token")
	}
}

func (h *Handler) RevokePersonalAccessToken(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	id, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "token id")
	if !ok {
		return
	}
	hash, err := h.Queries.RevokePersonalAccessToken(r.Context(), db.RevokePersonalAccessTokenParams{ID: id, UserID: parseUUID(userID)})
	switch {
	case err == nil:
		h.PATCache.Invalidate(r.Context(), hash)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		writeError(w, http.StatusInternalServerError, "failed to revoke token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

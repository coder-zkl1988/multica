package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const serviceAccountTokenTTL = 90 * 24 * time.Hour

type serviceAccountResponse struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Email       string     `json:"email"`
	WorkspaceID string     `json:"workspace_id"`
	Role        string     `json:"role"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	Token       string     `json:"token,omitempty"`
}

func (h *Handler) CreateServiceAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var request struct {
		Email string `json:"email"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email {
		writeError(w, http.StatusBadRequest, "valid email is required")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	user, err := queries.CreateServiceAccountUser(r.Context(), email)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email is already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	if _, err := queries.CreateMember(r.Context(), db.CreateMemberParams{WorkspaceID: workspaceID, UserID: user.ID, Role: "admin"}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	rawToken, tokenRow, err := createServiceAccountToken(r.Context(), queries, user.ID, workspaceID, parseUUID(creatorID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account token")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create service account")
		return
	}
	writeJSON(w, http.StatusCreated, serviceAccountToResponse(user, workspaceID, &tokenRow, rawToken))
}

func (h *Handler) GetServiceAccount(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	user, err := h.Queries.GetServiceAccountUserByWorkspace(r.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "service account not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to get service account")
		return
	}
	token, err := h.Queries.GetActiveServiceAccountTokenByWorkspace(r.Context(), workspaceID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeJSON(w, http.StatusOK, serviceAccountToResponse(user, workspaceID, nil, ""))
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to get service account")
		return
	}
	writeJSON(w, http.StatusOK, serviceAccountToResponse(user, workspaceID, &token, ""))
}

func (h *Handler) RotateServiceAccountToken(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	creatorID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate service account token")
		return
	}
	defer tx.Rollback(r.Context())
	queries := h.Queries.WithTx(tx)
	user, err := queries.GetServiceAccountUserByWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusNotFound, "service account not found")
		return
	}
	if err := queries.RevokeActiveServiceAccountTokens(r.Context(), db.RevokeActiveServiceAccountTokensParams{UserID: user.ID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate service account token")
		return
	}
	rawToken, tokenRow, err := createServiceAccountToken(r.Context(), queries, user.ID, workspaceID, parseUUID(creatorID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate service account token")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to rotate service account token")
		return
	}
	writeJSON(w, http.StatusOK, serviceAccountToResponse(user, workspaceID, &tokenRow, rawToken))
}

func (h *Handler) RevokeServiceAccountToken(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace id")
	if !ok {
		return
	}
	user, err := h.Queries.GetServiceAccountUserByWorkspace(r.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to revoke service account token")
		return
	}
	if err := h.Queries.RevokeActiveServiceAccountTokens(r.Context(), db.RevokeActiveServiceAccountTokensParams{UserID: user.ID, WorkspaceID: workspaceID}); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to revoke service account token")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func createServiceAccountToken(ctx context.Context, queries *db.Queries, userID, workspaceID, creatorID pgtype.UUID) (string, db.ServiceAccountToken, error) {
	raw, err := auth.GenerateServiceAccountToken()
	if err != nil {
		return "", db.ServiceAccountToken{}, err
	}
	token, err := queries.CreateServiceAccountToken(ctx, db.CreateServiceAccountTokenParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
		TokenHash:   auth.HashToken(raw),
		ExpiresAt:   pgtype.Timestamptz{Time: time.Now().Add(serviceAccountTokenTTL), Valid: true},
		CreatedBy:   creatorID,
	})
	return raw, token, err
}

func serviceAccountToResponse(user db.User, workspaceID pgtype.UUID, token *db.ServiceAccountToken, raw string) serviceAccountResponse {
	response := serviceAccountResponse{
		ID:          uuidToString(user.ID),
		Name:        user.Name,
		Email:       user.Email,
		WorkspaceID: uuidToString(workspaceID),
		Role:        "admin",
		Token:       raw,
	}
	if token != nil {
		response.ExpiresAt = &token.ExpiresAt.Time
		if token.LastUsedAt.Valid {
			response.LastUsedAt = &token.LastUsedAt.Time
		}
	}
	return response
}

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/middleware"
)

func TestServiceAccountLifecycle(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	cleanup := func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE account_kind = 'service'`)
	}
	cleanup()
	t.Cleanup(cleanup)
	email := fmt.Sprintf("ai-work-%d@soyoung.com", time.Now().UnixNano())
	request := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/service-account", map[string]string{"email": email})
	request = withURLParams(request, "id", testWorkspaceID)
	request.Header.Set("X-User-ID", testUserID)
	recorder := httptest.NewRecorder()
	testHandler.CreateServiceAccount(recorder, request)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var created struct {
		ID          string    `json:"id"`
		Name        string    `json:"name"`
		Email       string    `json:"email"`
		WorkspaceID string    `json:"workspace_id"`
		Role        string    `json:"role"`
		Token       string    `json:"token"`
		ExpiresAt   time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "ai_work" || created.Email != email || created.WorkspaceID != testWorkspaceID || created.Role != "admin" || !strings.HasPrefix(created.Token, "msa_") {
		t.Fatalf("created service account = %#v", created)
	}
	if remaining := time.Until(created.ExpiresAt); remaining < 89*24*time.Hour || remaining > 90*24*time.Hour {
		t.Fatalf("token remaining lifetime = %v", remaining)
	}
	var accountKind, role, storedHash string
	if err := testPool.QueryRow(t.Context(), `
		SELECT u.account_kind, m.role, sat.token_hash
		FROM "user" u
		JOIN member m ON m.user_id = u.id
		JOIN service_account_token sat ON sat.user_id = u.id
		WHERE u.id = $1
	`, created.ID).Scan(&accountKind, &role, &storedHash); err != nil {
		t.Fatal(err)
	}
	if accountKind != "service" || role != "admin" || storedHash == created.Token || storedHash == "" {
		t.Fatalf("persisted kind=%q role=%q hash=%q", accountKind, role, storedHash)
	}

	secondRequest := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/service-account", map[string]string{"email": "another-ai-work@soyoung.com"})
	secondRequest = withURLParams(secondRequest, "id", testWorkspaceID)
	secondRequest.Header.Set("X-User-ID", testUserID)
	secondRecorder := httptest.NewRecorder()
	testHandler.CreateServiceAccount(secondRecorder, secondRequest)
	if secondRecorder.Code != http.StatusConflict {
		t.Fatalf("second service account status = %d, want 409", secondRecorder.Code)
	}

	assertToken := func(token string, useSySSO bool, wantStatus int) {
		t.Helper()
		var actorSource, workspaceID string
		handler := middleware.Auth(testHandler.Queries, nil, nil, useSySSO)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			actorSource = r.Header.Get("X-Actor-Source")
			workspaceID = r.Header.Get("X-Service-Workspace-ID")
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("token status = %d, want %d", rec.Code, wantStatus)
		}
		if wantStatus == http.StatusOK && (actorSource != "service_account" || workspaceID != testWorkspaceID) {
			t.Fatalf("token actor source/workspace = %q/%q", actorSource, workspaceID)
		}
	}
	assertToken(created.Token, false, http.StatusUnauthorized)
	assertToken(created.Token, true, http.StatusOK)
	assertDaemonToken := func(useSySSO bool, wantStatus int) {
		t.Helper()
		var workspaceID string
		handler := middleware.DaemonAuth(testHandler.Queries, nil, nil, nil, useSySSO)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			workspaceID = middleware.DaemonWorkspaceIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))
		req := httptest.NewRequest(http.MethodPost, "/api/daemon/heartbeat", nil)
		req.Header.Set("Authorization", "Bearer "+created.Token)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		if recorder.Code != wantStatus {
			t.Fatalf("daemon token status = %d, want %d", recorder.Code, wantStatus)
		}
		if wantStatus == http.StatusOK && workspaceID != testWorkspaceID {
			t.Fatalf("daemon workspace = %q, want %q", workspaceID, testWorkspaceID)
		}
	}
	assertDaemonToken(false, http.StatusUnauthorized)
	assertDaemonToken(true, http.StatusOK)

	rotateRequest := newRequest(http.MethodPost, "/api/workspaces/"+testWorkspaceID+"/service-account/rotate", nil)
	rotateRequest = withURLParams(rotateRequest, "id", testWorkspaceID)
	rotateRequest.Header.Set("X-User-ID", testUserID)
	rotateRecorder := httptest.NewRecorder()
	testHandler.RotateServiceAccountToken(rotateRecorder, rotateRequest)
	if rotateRecorder.Code != http.StatusOK {
		t.Fatalf("rotate status = %d, body = %s", rotateRecorder.Code, rotateRecorder.Body.String())
	}
	var rotated struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(rotateRecorder.Body.Bytes(), &rotated); err != nil {
		t.Fatal(err)
	}
	if rotated.Token == "" || rotated.Token == created.Token {
		t.Fatalf("rotated token = %q", rotated.Token)
	}
	assertToken(created.Token, true, http.StatusUnauthorized)
	assertToken(rotated.Token, true, http.StatusOK)

	revokeRequest := newRequest(http.MethodDelete, "/api/workspaces/"+testWorkspaceID+"/service-account", nil)
	revokeRequest = withURLParams(revokeRequest, "id", testWorkspaceID)
	revokeRequest.Header.Set("X-User-ID", testUserID)
	revokeRecorder := httptest.NewRecorder()
	testHandler.RevokeServiceAccountToken(revokeRecorder, revokeRequest)
	if revokeRecorder.Code != http.StatusNoContent {
		t.Fatalf("revoke status = %d, body = %s", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	assertToken(rotated.Token, true, http.StatusUnauthorized)
}

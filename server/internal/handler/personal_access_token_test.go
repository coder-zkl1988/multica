package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/auth"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func insertTestPAT(t *testing.T, expiresAt time.Time) (string, string) {
	t.Helper()
	raw, err := auth.GeneratePATToken()
	if err != nil {
		t.Fatal(err)
	}
	prefix := raw
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	pat, err := testHandler.Queries.CreatePersonalAccessToken(context.Background(), db.CreatePersonalAccessTokenParams{
		UserID:      parseUUID(testUserID),
		Name:        "renew-test",
		TokenHash:   auth.HashToken(raw),
		TokenPrefix: prefix,
		ExpiresAt:   pgtype.Timestamptz{Time: expiresAt, Valid: !expiresAt.IsZero()},
	})
	if err != nil {
		t.Fatal(err)
	}
	patID := uuidToString(pat.ID)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM personal_access_token WHERE id = $1`, pat.ID)
	})
	return raw, patID
}

func newRenewRequest(rawToken string) *http.Request {
	req := newRequest(http.MethodPost, "/api/tokens/current/renew", nil)
	if rawToken != "" {
		req.Header.Set("Authorization", "Bearer "+rawToken)
	}
	return req
}

func decodeRenewResponse(t *testing.T, recorder *httptest.ResponseRecorder) RenewPATResponse {
	t.Helper()
	var response RenewPATResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return response
}

func TestRenewPAT_ExtendsWhenInsideRenewalWindow(t *testing.T) {
	oldExpiry := time.Now().Add(3 * 24 * time.Hour)
	raw, patID := insertTestPAT(t, oldExpiry)
	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if response := decodeRenewResponse(t, w); !response.Renewed {
		t.Fatalf("expected renewed=true: %#v", response)
	}
	var actual time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID)).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if !actual.After(oldExpiry.Add(24 * time.Hour)) {
		t.Fatalf("expiry was not extended: %v", actual)
	}
}

func TestRenewPAT_NoOpWhenOutsideRenewalWindow(t *testing.T) {
	oldExpiry := time.Now().Add(30 * 24 * time.Hour).Truncate(time.Second)
	raw, patID := insertTestPAT(t, oldExpiry)
	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if response := decodeRenewResponse(t, w); response.Renewed {
		t.Fatalf("expected renewed=false: %#v", response)
	}
	var actual time.Time
	if err := testPool.QueryRow(context.Background(), `SELECT expires_at FROM personal_access_token WHERE id = $1`, parseUUID(patID)).Scan(&actual); err != nil {
		t.Fatal(err)
	}
	if !actual.Equal(oldExpiry) {
		t.Fatalf("expiry changed: %v -> %v", oldExpiry, actual)
	}
}

func TestRenewPAT_RejectsInvalidTokens(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"empty", "", http.StatusBadRequest},
		{"wrong prefix", "Bearer mdt_abc", http.StatusBadRequest},
		{"expired", "", http.StatusUnauthorized},
	}
	for _, tc := range tests[:2] {
		t.Run(tc.name, func(t *testing.T) {
			req := newRequest(http.MethodPost, "/api/tokens/current/renew", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			testHandler.RenewCurrentPersonalAccessToken(w, req)
			if w.Code != tc.want {
				t.Fatalf("expected %d, got %d: %s", tc.want, w.Code, w.Body.String())
			}
		})
	}
	raw, _ := insertTestPAT(t, time.Now().Add(-time.Hour))
	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expired token: expected 401, got %d", w.Code)
	}
}

func TestRenewPAT_HandlesNullExpiresAt(t *testing.T) {
	raw, _ := insertTestPAT(t, time.Time{})
	w := httptest.NewRecorder()
	testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
	response := decodeRenewResponse(t, w)
	if w.Code != http.StatusOK || response.Renewed || response.ExpiresAt != "" {
		t.Fatalf("response = %#v, status %d", response, w.Code)
	}
}

func TestRenewPAT_ParallelRenewExtendsExactlyOnce(t *testing.T) {
	const concurrency = 8
	raw, _ := insertTestPAT(t, time.Now().Add(2*24*time.Hour))
	type result struct {
		code    int
		renewed bool
	}
	results := make([]result, concurrency)
	start := make(chan struct{})
	var group sync.WaitGroup
	group.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(index int) {
			defer group.Done()
			<-start
			w := httptest.NewRecorder()
			testHandler.RenewCurrentPersonalAccessToken(w, newRenewRequest(raw))
			var response RenewPATResponse
			_ = json.NewDecoder(w.Body).Decode(&response)
			results[index] = result{code: w.Code, renewed: response.Renewed}
		}(i)
	}
	close(start)
	group.Wait()
	winners := 0
	for _, result := range results {
		if result.code != http.StatusOK {
			t.Fatalf("renew status = %d", result.code)
		}
		if result.renewed {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("renew winners = %d, want 1", winners)
	}
}

func TestRevokePersonalAccessTokenRejectsMalformedID(t *testing.T) {
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodDelete, "/api/tokens/not-a-uuid", nil), "id", "not-a-uuid")
	testHandler.RevokePersonalAccessToken(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

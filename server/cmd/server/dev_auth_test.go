package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestRouterDevAuthEmail(t *testing.T) {
	if testPool == nil {
		t.Skip("DATABASE_URL not set")
	}
	email := fmt.Sprintf("router-dev-sso-%d@example.com", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = testPool.Exec(t.Context(), `DELETE FROM "user" WHERE email = $1`, email)
	})

	router, _ := NewRouterWithOptions(
		testPool,
		realtime.NewHub(),
		events.New(),
		analytics.NoopClient{},
		nil,
		RouterOptions{UseSySSO: true, DevAuthEmail: email},
	)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth/sso/session", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("dev SSO route status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

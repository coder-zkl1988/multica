package main

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestDesignAssetRepositoryAssociationRouteIsRegistered(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	found := false
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if method == http.MethodPut && route == "/api/design-assets/repository-association" {
			found = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk router: %v", err)
	}
	if !found {
		t.Fatalf("%s %s is not registered", http.MethodPut, "/api/design-assets/repository-association")
	}
}

package main

import (
	"net/http"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/realtime"
)

func TestDesignAssetFramesRouteIsRegistered(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	found := false
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		found = found || method == http.MethodGet && route == "/api/design-assets/{designRef}/frames"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("GET /api/design-assets/{designRef}/frames is not registered")
	}
}

func TestDesignAssetImplementationPromptRouteIsRegistered(t *testing.T) {
	router := NewRouter(nil, realtime.NewHub(), events.New(), analytics.NoopClient{}, nil)
	promptFound := false
	contextFound := false
	if err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		promptFound = promptFound || method == http.MethodPost && route == "/api/design-assets/{designRef}/implementation-prompt"
		contextFound = contextFound || method == http.MethodPost && route == "/api/design-assets/{designRef}/implementation-context"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !promptFound || !contextFound {
		t.Fatalf("implementation routes registered: prompt=%v context=%v", promptFound, contextFound)
	}
}

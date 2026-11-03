package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// createRecipeGateRecipe inserts one published catalogue recipe of the given
// mode for the test workspace.
func createRecipeGateRecipe(t *testing.T, slug, mode string) {
	t.Helper()
	dbfx.Insert(t, "design_scenario_recipe", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"slug":         slug,
		"title":        slug,
		"summary":      "recipe for the create gate test",
		"category":     "测试",
		"mode":         mode,
		"prompt":       "测试配方。",
		"origin":       "workspace",
		"published_at": time.Now().UTC(),
	})
}

// A published deck recipe creates a document exactly like a prototype recipe:
// the built-in deck chip has always shipped over this pipeline, and a
// workspace's deck recipes are the same package format with slides as its
// pages. A mode without a producer is still turned away at the gate — the
// schema's other enum values stay data, not features.
func TestCreateDesignDocumentResolvesPublishedRecipeModes(t *testing.T) {
	projectID := createProjectDesignSystemProject(t, testWorkspaceID, "Recipe gate project")
	agentID, _ := createProjectDesignSystemAgent(t, "online")
	createRecipeGateRecipe(t, "gate-pitch-deck", "deck")
	createRecipeGateRecipe(t, "gate-promo-video", "video")

	createBody := func(recipe string) map[string]any {
		return map[string]any{
			"project_id": projectID,
			"agent_id":   agentID,
			"platform":   "web",
			"recipe":     recipe,
			"brief":      "产品路演幻灯片，十页以内。",
			"title":      "路演幻灯片",
		}
	}

	t.Run("deck", func(t *testing.T) {
		var created DesignDocumentResponse
		testutil.Call(t, testHandler.CreateDesignDocument,
			newRequest(http.MethodPost, "/api/design-documents", createBody("gate-pitch-deck")),
		).Want(http.StatusCreated).JSON(&created)
		if created.Recipe != "gate-pitch-deck" {
			t.Fatalf("document recipe = %q, want gate-pitch-deck", created.Recipe)
		}
		dbfx.Cleanup(t, `DELETE FROM design_document WHERE id = $1`, parseUUID(created.ID))
	})

	t.Run("video", func(t *testing.T) {
		var failure struct {
			Code  string `json:"code"`
			Error string `json:"error"`
		}
		testutil.Call(t, testHandler.CreateDesignDocument,
			newRequest(http.MethodPost, "/api/design-documents", createBody("gate-promo-video")),
		).Want(http.StatusBadRequest).JSON(&failure)
		if failure.Code != "recipe_mode_unsupported" {
			t.Fatalf("rejection = %q (%s), want recipe_mode_unsupported", failure.Code, failure.Error)
		}
	})
}

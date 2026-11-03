package handler

import (
	"net/http"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/testutil"
)

// createFavoriteRecipe inserts one published catalogue recipe for the test
// workspace and returns its id.
func createFavoriteRecipe(t *testing.T, slug string) string {
	t.Helper()
	return dbfx.Insert(t, "design_scenario_recipe", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"slug":         slug,
		"title":        slug,
		"summary":      "recipe for the favourite tests",
		"category":     "测试",
		"mode":         "prototype",
		"prompt":       "测试配方。",
		"origin":       "workspace",
		"published_at": time.Now().UTC(),
	})
}

// listRecipes calls the catalogue and indexes the answer by slug.
func listRecipes(t *testing.T) map[string]DesignScenarioRecipeResponse {
	t.Helper()
	var listed struct {
		Recipes []DesignScenarioRecipeResponse `json:"recipes"`
	}
	testutil.Call(t, testHandler.ListDesignScenarioRecipes,
		newRequest(http.MethodGet, "/api/design-recipes", nil),
	).Want(http.StatusOK).JSON(&listed)
	bySlug := make(map[string]DesignScenarioRecipeResponse, len(listed.Recipes))
	for _, recipe := range listed.Recipes {
		bySlug[recipe.Slug] = recipe
	}
	return bySlug
}

func favoriteRequest(method, recipeID string) *http.Request {
	return withURLParam(
		newRequest(method, "/api/design-recipes/"+recipeID+"/favorite", nil),
		"id", recipeID,
	)
}

// A save is per requester and round-trips through the listing: the flag is
// what the gallery filters on after a reload, so it must survive the request
// that wrote it without a second endpoint.
func TestDesignScenarioRecipeFavoriteRoundTripsThroughTheListing(t *testing.T) {
	recipeID := createFavoriteRecipe(t, "favorite-round-trip")
	dbfx.Cleanup(t, `DELETE FROM design_scenario_recipe_favorite WHERE user_id = $1`, testUserID)

	if got := listRecipes(t)["favorite-round-trip"]; got.Favorited {
		t.Fatalf("listing before save: favorited = true, want false (id=%q)", got.ID)
	}

	var saved struct {
		Favorited bool `json:"favorited"`
	}
	testutil.Call(t, testHandler.FavoriteDesignScenarioRecipe,
		favoriteRequest(http.MethodPost, recipeID),
	).Want(http.StatusOK).JSON(&saved)
	if !saved.Favorited {
		t.Fatal("POST did not report favorited")
	}

	got := listRecipes(t)["favorite-round-trip"]
	if !got.Favorited {
		t.Fatal("listing after save: favorited = false, want true")
	}
	if got.ID != recipeID {
		t.Fatalf("listing id = %q, want %q", got.ID, recipeID)
	}

	var removed struct {
		Favorited bool `json:"favorited"`
	}
	testutil.Call(t, testHandler.UnfavoriteDesignScenarioRecipe,
		favoriteRequest(http.MethodDelete, recipeID),
	).Want(http.StatusOK).JSON(&removed)
	if removed.Favorited {
		t.Fatal("DELETE did not report unfavorited")
	}
	if listRecipes(t)["favorite-round-trip"].Favorited {
		t.Fatal("listing after delete: favorited = true, want false")
	}
}

// Both directions are idempotent: a retried request from a flaky network must
// not turn into an error, and repeated saves must not duplicate the row the
// unique index is there to prevent.
func TestDesignScenarioRecipeFavoriteIsIdempotent(t *testing.T) {
	recipeID := createFavoriteRecipe(t, "favorite-idempotent")
	dbfx.Cleanup(t, `DELETE FROM design_scenario_recipe_favorite WHERE user_id = $1`, testUserID)

	for range 2 {
		testutil.Call(t, testHandler.FavoriteDesignScenarioRecipe,
			favoriteRequest(http.MethodPost, recipeID),
		).Want(http.StatusOK)
	}
	if n := dbfx.Count(t,
		`SELECT count(*) FROM design_scenario_recipe_favorite WHERE user_id = $1 AND recipe_id = $2`,
		testUserID, recipeID); n != 1 {
		t.Fatalf("favorite rows after two saves = %d, want 1", n)
	}

	for range 2 {
		testutil.Call(t, testHandler.UnfavoriteDesignScenarioRecipe,
			favoriteRequest(http.MethodDelete, recipeID),
		).Want(http.StatusOK)
	}
	if n := dbfx.Count(t,
		`SELECT count(*) FROM design_scenario_recipe_favorite WHERE user_id = $1 AND recipe_id = $2`,
		testUserID, recipeID); n != 0 {
		t.Fatalf("favorite rows after two removals = %d, want 0", n)
	}
}

// The toggle reuses the listing's visibility gate: an id this workspace cannot
// list is indistinguishable from one that does not exist.
func TestDesignScenarioRecipeFavoriteRejectsInvisibleAndMalformedIDs(t *testing.T) {
	testutil.Call(t, testHandler.FavoriteDesignScenarioRecipe,
		favoriteRequest(http.MethodPost, "00000000-0000-0000-0000-000000000000"),
	).Want(http.StatusNotFound)

	testutil.Call(t, testHandler.UnfavoriteDesignScenarioRecipe,
		favoriteRequest(http.MethodDelete, "not-a-uuid"),
	).Want(http.StatusBadRequest)
}

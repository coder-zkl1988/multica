package handler

import (
	"errors"
	"github.com/multica-ai/multica/server/internal/designrecipepreview"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// The design centre's community tab (DC-041 / DC-048).
//
// A recipe is a page-design task configuration, not a design asset. Picking
// one seeds the composer's brief; it does not produce anything by itself. That
// is why applying a recipe is not an endpoint — the composer already has a
// create call, and the recipe only decides what it is pre-filled with.

type DesignScenarioRecipeResponse struct {
	// The catalogue row's id. Favourite toggles address it, so two rows that
	// share a slug (a workspace's override and its built-in) stay distinct.
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Title       string `json:"title"`
	Summary     string `json:"summary"`
	Category    string `json:"category"`
	Subcategory string `json:"subcategory,omitempty"`
	Mode        string `json:"mode"`
	Platform    string `json:"platform,omitempty"`
	// The brief the composer is pre-filled with. Sent with the listing so
	// picking a card does not need a second round trip.
	Prompt string `json:"prompt"`
	// Relative media path for the card image, empty when the recipe has none.
	PreviewPath string `json:"preview_path,omitempty"`
	// Cover a built-in recipe ships with: "html" renders the template's own
	// example output in a sandboxed frame, "poster" is a still. Empty when it
	// has neither — the card then falls back to its mode icon.
	PreviewKind string `json:"preview_kind,omitempty"`
	// API path of that cover, digest included, so the client frames the URL
	// the server composed rather than one it guessed; empty with PreviewKind.
	PreviewURL  string `json:"preview_url,omitempty"`
	Origin      string `json:"origin"`
	PublishedAt string `json:"published_at,omitempty"`
	// Whether the requester has saved this recipe. Read on every listing so a
	// reload restores the saved filter without a second round trip.
	Favorited bool `json:"favorited"`
}

// ListDesignScenarioRecipes returns the published catalogue visible to this
// workspace: everything built in, plus the workspace's own.
func (h *Handler) ListDesignScenarioRecipes(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPublishedDesignScenarioRecipes(r.Context(), workspaceUUID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load the design recipe catalogue")
		return
	}
	// Favourite state rides the listing instead of a second endpoint: the
	// gallery already needs the whole catalogue, and a separate favourites
	// list would make the Saved filter wait on a second request and would
	// have to be merged by the client anyway.
	favoriteIDs, err := h.Queries.ListDesignScenarioRecipeFavorites(r.Context(), requesterID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load saved design recipes")
		return
	}
	favorited := make(map[pgtype.UUID]bool, len(favoriteIDs))
	for _, id := range favoriteIDs {
		favorited[id] = true
	}
	// A workspace recipe shadows a built-in with the same slug: the workspace
	// deliberately overrode it, so showing both would be confusing and would
	// make the two cards behave identically anyway.
	bySlug := make(map[string]db.DesignScenarioRecipe, len(rows))
	order := make([]string, 0, len(rows))
	for _, row := range rows {
		existing, seen := bySlug[row.Slug]
		if !seen {
			order = append(order, row.Slug)
			bySlug[row.Slug] = row
			continue
		}
		if !existing.WorkspaceID.Valid && row.WorkspaceID.Valid {
			bySlug[row.Slug] = row
		}
	}
	recipes := make([]DesignScenarioRecipeResponse, 0, len(order))
	for _, slug := range order {
		recipes = append(recipes, designScenarioRecipeResponse(bySlug[slug], favorited))
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes})
}

func designScenarioRecipeResponse(row db.DesignScenarioRecipe, favorited map[pgtype.UUID]bool) DesignScenarioRecipeResponse {
	response := DesignScenarioRecipeResponse{
		ID:        uuidToString(row.ID),
		Favorited: favorited[row.ID],
		Slug:      row.Slug,
		Title:     row.Title,
		Summary:   row.Summary,
		Category:  row.Category,
		Mode:      row.Mode,
		Prompt:    row.Prompt,
		Origin:    row.Origin,
	}
	if row.Subcategory.Valid {
		response.Subcategory = row.Subcategory.String
	}
	if row.Platform.Valid {
		response.Platform = row.Platform.String
	}
	if row.PreviewObjectKey.Valid {
		response.PreviewPath = row.PreviewObjectKey.String
	}
	// Built-ins carry their cover in the binary; a workspace's own recipe has
	// only what it uploaded to PreviewObjectKey.
	if row.Origin == "builtin" {
		response.PreviewKind = string(designrecipepreview.KindFor(row.Slug))
		response.PreviewURL = designRecipePreviewPath(row.Slug)
	}
	if row.PublishedAt.Valid {
		response.PublishedAt = row.PublishedAt.Time.UTC().Format(time.RFC3339Nano)
	}
	return response
}

// resolveDesignDocumentRecipe validates the recipe a create request names.
// The five scenario chips are built into the composer and always valid; any
// other value must resolve to a published recipe this workspace can see, so a
// document can never record a recipe that does not exist.
func (h *Handler) resolveDesignDocumentRecipe(
	r *http.Request,
	w http.ResponseWriter,
	workspaceID pgtype.UUID,
	recipe string,
) (string, bool) {
	recipe = strings.TrimSpace(recipe)
	if recipe == "" {
		return designDocumentDefaultRecipe, true
	}
	if _, builtin := designDocumentRecipes[recipe]; builtin {
		return recipe, true
	}
	row, err := h.Queries.GetPublishedDesignScenarioRecipeBySlug(r.Context(), db.GetPublishedDesignScenarioRecipeBySlugParams{
		Slug:        recipe,
		WorkspaceID: workspaceID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "recipe_invalid", "recipe is not a known design scenario")
		return "", false
	}
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "recipe_lookup_failed", "failed to resolve the design recipe")
		return "", false
	}
	// Prototype and deck recipes drive a page-design task: a deck is the same
	// package pipeline with slides as its pages, matching what the built-in
	// deck chip already produces. The remaining modes exist in the schema but
	// have no producer yet.
	if row.Mode != "prototype" && row.Mode != "deck" {
		writeProjectDesignSystemError(w, http.StatusBadRequest, "recipe_mode_unsupported", "this recipe produces an artifact kind that is not supported yet")
		return "", false
	}
	return row.Slug, true
}

// FavoriteDesignScenarioRecipe saves a catalogue recipe for the requester.
// Toggle-style and idempotent: saving an already-saved recipe is the same
// state, not an error, so a retried request from a flaky network cannot leave
// the button in a state the server disagrees with.
func (h *Handler) FavoriteDesignScenarioRecipe(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	recipeUUID, ok := h.resolveFavoriteRecipe(w, r, workspaceUUID)
	if !ok {
		return
	}
	if err := h.Queries.AddDesignScenarioRecipeFavorite(r.Context(), db.AddDesignScenarioRecipeFavoriteParams{
		UserID:   requesterID,
		RecipeID: recipeUUID,
	}); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "favorite_failed", "failed to save the design recipe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorited": true})
}

// UnfavoriteDesignScenarioRecipe removes the requester's save. Also
// idempotent, and it never reports whether a row existed: the caller only
// needs the resulting state.
func (h *Handler) UnfavoriteDesignScenarioRecipe(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, requesterID, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	recipeUUID, ok := h.resolveFavoriteRecipe(w, r, workspaceUUID)
	if !ok {
		return
	}
	if err := h.Queries.RemoveDesignScenarioRecipeFavorite(r.Context(), db.RemoveDesignScenarioRecipeFavoriteParams{
		UserID:   requesterID,
		RecipeID: recipeUUID,
	}); err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "unfavorite_failed", "failed to remove the saved design recipe")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"favorited": false})
}

// resolveFavoriteRecipe applies the same visibility gate the listing uses, so
// a recipe the workspace cannot see cannot be saved by naming its id.
func (h *Handler) resolveFavoriteRecipe(w http.ResponseWriter, r *http.Request, workspaceUUID pgtype.UUID) (pgtype.UUID, bool) {
	recipeUUID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "recipe_id")
	if !ok {
		return pgtype.UUID{}, false
	}
	if _, err := h.Queries.GetPublishedDesignScenarioRecipeByID(r.Context(), db.GetPublishedDesignScenarioRecipeByIDParams{
		ID:          recipeUUID,
		WorkspaceID: workspaceUUID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeProjectDesignSystemError(w, http.StatusNotFound, "recipe_not_found", "design recipe not found")
			return pgtype.UUID{}, false
		}
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "recipe_lookup_failed", "failed to resolve the design recipe")
		return pgtype.UUID{}, false
	}
	return recipeUUID, true
}

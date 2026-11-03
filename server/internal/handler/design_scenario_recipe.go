package handler

import (
	"errors"
	"github.com/multica-ai/multica/server/internal/designrecipepreview"
	"net/http"
	"strings"
	"time"

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
}

// ListDesignScenarioRecipes returns the published catalogue visible to this
// workspace: everything built in, plus the workspace's own.
func (h *Handler) ListDesignScenarioRecipes(w http.ResponseWriter, r *http.Request) {
	workspaceUUID, _, ok := h.projectDesignSystemRequestScope(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListPublishedDesignScenarioRecipes(r.Context(), workspaceUUID)
	if err != nil {
		writeProjectDesignSystemError(w, http.StatusInternalServerError, "lookup_failed", "failed to load the design recipe catalogue")
		return
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
		recipes = append(recipes, designScenarioRecipeResponse(bySlug[slug]))
	}
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes})
}

func designScenarioRecipeResponse(row db.DesignScenarioRecipe) DesignScenarioRecipeResponse {
	response := DesignScenarioRecipeResponse{
		Slug:     row.Slug,
		Title:    row.Title,
		Summary:  row.Summary,
		Category: row.Category,
		Mode:     row.Mode,
		Prompt:   row.Prompt,
		Origin:   row.Origin,
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

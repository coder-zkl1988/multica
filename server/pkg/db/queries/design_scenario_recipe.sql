-- Design scenario recipes (DC-041 / DC-049).
--
-- Every read is scoped to "built-in OR this workspace's own": a recipe with a
-- NULL workspace_id ships with the product, anything else belongs to exactly
-- one workspace and must never leak across the boundary.

-- name: ListPublishedDesignScenarioRecipes :many
SELECT * FROM design_scenario_recipe
WHERE published_at IS NOT NULL
  AND (workspace_id IS NULL OR workspace_id = sqlc.arg('workspace_id'))
ORDER BY category, position, created_at;

-- name: GetPublishedDesignScenarioRecipeBySlug :one
SELECT * FROM design_scenario_recipe
WHERE slug = sqlc.arg('slug')
  AND published_at IS NOT NULL
  AND (workspace_id IS NULL OR workspace_id = sqlc.arg('workspace_id'))
-- A workspace's own recipe shadows a built-in with the same slug, so the
-- workspace row sorts first.
ORDER BY (workspace_id IS NULL)
LIMIT 1;

-- The favourite endpoints resolve the id through this same visibility gate, so
-- a person can only save a recipe their workspace can already list.
-- name: GetPublishedDesignScenarioRecipeByID :one
SELECT * FROM design_scenario_recipe
WHERE id = sqlc.arg('id')
  AND published_at IS NOT NULL
  AND (workspace_id IS NULL OR workspace_id = sqlc.arg('workspace_id'));

-- name: ListDesignScenarioRecipeFavorites :many
SELECT recipe_id FROM design_scenario_recipe_favorite
WHERE user_id = sqlc.arg('user_id');

-- name: AddDesignScenarioRecipeFavorite :exec
INSERT INTO design_scenario_recipe_favorite (user_id, recipe_id)
VALUES (sqlc.arg('user_id'), sqlc.arg('recipe_id'))
ON CONFLICT (user_id, recipe_id) DO NOTHING;

-- name: RemoveDesignScenarioRecipeFavorite :exec
DELETE FROM design_scenario_recipe_favorite
WHERE user_id = sqlc.arg('user_id')
  AND recipe_id = sqlc.arg('recipe_id');

-- name: UpsertBuiltinDesignScenarioRecipe :one
INSERT INTO design_scenario_recipe (
    workspace_id, slug, title, summary, category, subcategory,
    mode, platform, prompt, preview_object_key, origin, published_at, position
) VALUES (
    NULL,
    sqlc.arg('slug'),
    sqlc.arg('title'),
    sqlc.arg('summary'),
    sqlc.arg('category'),
    sqlc.narg('subcategory'),
    sqlc.arg('mode'),
    sqlc.narg('platform'),
    sqlc.arg('prompt'),
    sqlc.narg('preview_object_key'),
    'builtin',
    now(),
    sqlc.arg('position')
)
ON CONFLICT (slug) WHERE workspace_id IS NULL DO UPDATE SET
    title = EXCLUDED.title,
    summary = EXCLUDED.summary,
    category = EXCLUDED.category,
    subcategory = EXCLUDED.subcategory,
    mode = EXCLUDED.mode,
    platform = EXCLUDED.platform,
    prompt = EXCLUDED.prompt,
    preview_object_key = EXCLUDED.preview_object_key,
    position = EXCLUDED.position,
    updated_at = now()
RETURNING *;

-- name: DeleteWorkspaceDesignScenarioRecipes :exec
DELETE FROM design_scenario_recipe
WHERE workspace_id = sqlc.arg('workspace_id');

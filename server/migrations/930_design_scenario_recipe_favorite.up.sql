-- Per-user favourites over the community recipe catalogue (DC-067).
--
-- A row is one person saving one recipe for later. The listing resolves the
-- favourite flag on read, so the gallery filters client-side without a second
-- request and a reload restores the same state.
--
-- Deliberately user-scoped, not workspace-scoped: a recipe is a catalogue
-- entry (built-in rows have a NULL workspace_id and are visible everywhere),
-- so "saved" is a personal bookmark like a star, not a workspace resource.
-- The gallery always lists recipes through the workspace boundary, so a
-- favourite can never make a recipe visible that the workspace could not
-- already see.
--
-- No foreign keys per repository policy: user and recipe are resolved in
-- application code. Recipes are an immutable catalogue — no endpoint creates
-- or deletes a single recipe row, and the only delete path drops a whole
-- workspace's own recipes on workspace teardown. A favourite left behind by
-- that path is unreachable, because the listing only stamps favourites onto
-- rows it can see.
CREATE TABLE design_scenario_recipe_favorite (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    recipe_id UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS idx_design_scenario_recipe_favorite_user_recipe ON design_scenario_recipe_favorite (user_id, recipe_id);

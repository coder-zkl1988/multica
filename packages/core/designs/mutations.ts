import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { designKeys } from "./keys";
import { useWorkspaceId } from "../hooks";
import type { ListDesignScenarioRecipesResponse } from "../types";

/**
 * Save or unsave a community recipe for the current user (DC-067).
 *
 * Optimistic on purpose, and it meets the repository's four conditions:
 *
 * - The outcome is locally determinate: the new `favorited` value is exactly
 *   the argument, not something the server derives.
 * - The user stays on the gallery — the toggle never navigates.
 * - Failure is rare (a workspace member writing their own bookmark).
 * - Rollback is trivial: restore the snapshot taken before the patch.
 *
 * The catalogue list is the only cache that carries the flag, so the patch is
 * a single map over `recipes`; the rollback is the whole previous response.
 * `onSettled` re-reads the list, so a server that ended up disagreeing (a
 * recipe deleted between read and write, say) wins over the local guess.
 */
export function useToggleDesignScenarioRecipeFavorite() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  const listKey = designKeys.scenarioRecipes(wsId);

  return useMutation({
    mutationFn: ({ recipeId, favorited }: { recipeId: string; favorited: boolean }) =>
      favorited
        ? api.favoriteDesignScenarioRecipe(recipeId)
        : api.unfavoriteDesignScenarioRecipe(recipeId),
    onMutate: async ({ recipeId, favorited }) => {
      await qc.cancelQueries({ queryKey: listKey });
      const previous = qc.getQueryData<ListDesignScenarioRecipesResponse>(listKey);
      qc.setQueryData<ListDesignScenarioRecipesResponse>(listKey, (old) =>
        old
          ? {
              ...old,
              recipes: old.recipes.map((recipe) =>
                recipe.id === recipeId ? { ...recipe, favorited } : recipe,
              ),
            }
          : old,
      );
      return { previous };
    },
    onError: (_error, _variables, context) => {
      // Undo exactly what onMutate wrote. Without this the card keeps the
      // star after a failed write until the next refetch.
      if (context?.previous) qc.setQueryData(listKey, context.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: listKey });
    },
  });
}

/**
 * @vitest-environment jsdom
 */
import { beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { DesignScenarioRecipe, ListDesignScenarioRecipesResponse } from "../types";
import { useToggleDesignScenarioRecipeFavorite } from "./mutations";
import { designKeys } from "./keys";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const LIST_KEY = designKeys.scenarioRecipes("ws-1");

function recipe(overrides: Partial<DesignScenarioRecipe> = {}): DesignScenarioRecipe {
  return {
    id: "recipe-1",
    slug: "crm-console",
    title: "CRM 控制台",
    summary: "",
    category: "业务系统",
    subcategory: "",
    mode: "prototype",
    platform: "web",
    prompt: "p",
    preview_path: "",
    preview_kind: "",
    preview_url: "",
    origin: "builtin",
    published_at: "",
    favorited: false,
    ...overrides,
  };
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

function cache(queryClient: QueryClient): DesignScenarioRecipe[] {
  return (
    queryClient.getQueryData<ListDesignScenarioRecipesResponse>(LIST_KEY)?.recipes ?? []
  );
}

describe("useToggleDesignScenarioRecipeFavorite", () => {
  let queryClient: QueryClient;
  let favorite: ReturnType<typeof vi.fn>;
  let unfavorite: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    });
    favorite = vi.fn(async () => ({ favorited: true }));
    unfavorite = vi.fn(async () => ({ favorited: false }));
    setApiInstance({
      favoriteDesignScenarioRecipe: favorite,
      unfavoriteDesignScenarioRecipe: unfavorite,
    } as unknown as ApiClient);
    queryClient.setQueryData<ListDesignScenarioRecipesResponse>(LIST_KEY, {
      recipes: [recipe({ id: "recipe-1" }), recipe({ id: "recipe-2", slug: "landing" })],
    });
  });

  it("flips only the targeted row, before the request settles", async () => {
    const { result } = renderHook(() => useToggleDesignScenarioRecipeFavorite(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => result.current.mutate({ recipeId: "recipe-1", favorited: true }));

    // The patch is synchronous with onMutate's first await, so the star is on
    // before the server answers — that is the point of the optimism.
    await waitFor(() => expect(cache(queryClient)[0]?.favorited).toBe(true));
    expect(cache(queryClient)[1]?.favorited).toBe(false);
    await waitFor(() => expect(favorite).toHaveBeenCalledWith("recipe-1"));
  });

  it("calls the remove endpoint when saving is turned off", async () => {
    queryClient.setQueryData<ListDesignScenarioRecipesResponse>(LIST_KEY, {
      recipes: [recipe({ id: "recipe-1", favorited: true })],
    });
    const { result } = renderHook(() => useToggleDesignScenarioRecipeFavorite(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => result.current.mutate({ recipeId: "recipe-1", favorited: false }));

    await waitFor(() => expect(unfavorite).toHaveBeenCalledWith("recipe-1"));
    expect(favorite).not.toHaveBeenCalled();
  });

  it("restores the previous catalogue when the write fails", async () => {
    favorite.mockRejectedValue(new Error("offline"));
    const before = cache(queryClient);
    const { result } = renderHook(() => useToggleDesignScenarioRecipeFavorite(), {
      wrapper: createWrapper(queryClient),
    });

    act(() => result.current.mutate({ recipeId: "recipe-1", favorited: true }));

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(cache(queryClient)).toEqual(before);
  });
});

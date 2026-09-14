import { beforeEach, describe, expect, it, vi } from "vitest";
import { api } from "../api";
import { designRepositoryCatalogueOptions } from "./queries";
import { designKeys } from "./keys";

vi.mock("../api", () => ({ api: { listDesignRepositories: vi.fn() } }));

describe("workspace design repository catalogue", () => {
  beforeEach(() => {
    vi.mocked(api.listDesignRepositories).mockResolvedValue({
      repositories: [{
        id: "repo-1", project_id: "project-1", project_title: "CRM", label: "web",
        repository_url: "https://github.com/example/web", default_branch_hint: "main",
      }],
    } as never);
    vi.clearAllMocks();
  });

  it("uses a workspace key, strict network DTO, and empty fallback", async () => {
    const options = designRepositoryCatalogueOptions("ws-1");
    expect(options.queryKey).toEqual(designKeys.designRepositories("ws-1"));
    expect(options.enabled).toBe(true);
    await options.queryFn?.(undefined as never);
    expect(api.listDesignRepositories).toHaveBeenCalledTimes(1);
    expect(options.select?.({ repositories: [] } as never)).toEqual([]);
  });
});

import { afterEach, describe, expect, it, vi } from "vitest";
import { fetchLatestRelease } from "./github-release";

const SAMPLE_LATEST_ASSET = {
  name: "multica-desktop-0.4.17-sso.2-mac-arm64.dmg",
  browser_download_url:
    "https://github.com/coder-zkl1988/multica/releases/download/desktop-v0.4.17-sso.2/multica-desktop-0.4.17-sso.2-mac-arm64.dmg",
};

function releasePayload(overrides: {
  tag: string;
  publishedMinutesAgo?: number;
  asset?: { name: string; browser_download_url: string };
  prerelease?: boolean;
  draft?: boolean;
}) {
  const published = new Date(
    Date.now() - (overrides.publishedMinutesAgo ?? 0) * 60_000,
  ).toISOString();
  return {
    tag_name: overrides.tag,
    published_at: published,
    html_url: `https://github.com/coder-zkl1988/multica/releases/tag/${overrides.tag}`,
    prerelease: overrides.prerelease ?? false,
    draft: overrides.draft ?? false,
    assets: overrides.asset ? [overrides.asset] : [],
  };
}

function mockFetchWithReleases(releases: unknown[]) {
  const fetchMock = vi.fn().mockResolvedValue(
    new Response(JSON.stringify(releases), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    }),
  );
  vi.stubGlobal("fetch", fetchMock);
  return fetchMock;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("fetchLatestRelease", () => {
  it("uses the newest published SSO desktop release from the fork", async () => {
    const fetchMock = mockFetchWithReleases([
      releasePayload({ tag: "v0.4.16-sso.1", prerelease: true }),
      releasePayload({ tag: "desktop-v0.4.17-sso.3", draft: true }),
      releasePayload({
        tag: "desktop-v0.4.17-sso.2",
        asset: SAMPLE_LATEST_ASSET,
        prerelease: true,
      }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBe("desktop-v0.4.17-sso.2");
    expect(result.assets.macArm64Dmg).toBe(SAMPLE_LATEST_ASSET.browser_download_url);
    expect(fetchMock).toHaveBeenCalledWith(
      expect.stringContaining("coder-zkl1988/multica/releases?per_page=20"),
      expect.any(Object),
    );
  });

  it("returns an empty release shape when the API errors", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("rate limited", { status: 403 }),
    );
    vi.stubGlobal("fetch", fetchMock);
    const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

    const result = await fetchLatestRelease();
    expect(result).toEqual({
      version: null,
      publishedAt: null,
      htmlUrl: null,
      assets: {},
    });
    expect(warnSpy).toHaveBeenCalled();
    warnSpy.mockRestore();
  });

  it("returns an empty release shape for a draft", async () => {
    mockFetchWithReleases([
      releasePayload({ tag: "desktop-v0.4.16-sso.1", draft: true }),
    ]);

    const result = await fetchLatestRelease();
    expect(result.version).toBeNull();
    expect(result.assets).toEqual({});
  });
});

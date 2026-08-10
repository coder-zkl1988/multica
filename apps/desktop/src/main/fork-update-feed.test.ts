import { describe, expect, it, vi } from "vitest";
import {
  configureForkUpdateFeed,
  latestDesktopReleaseTagFromAtom,
} from "./fork-update-feed";

describe("latestDesktopReleaseTagFromAtom", () => {
  it("skips CLI releases and returns the newest desktop release", () => {
    expect(
      latestDesktopReleaseTagFromAtom(`
        <?xml version="1.0" encoding="UTF-8"?>
        <feed>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/v0.4.16-sso.1"/>
          </entry>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/desktop-v0.4.17-sso.5"/>
          </entry>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/desktop-v0.4.17-sso.4"/>
          </entry>
        </feed>
      `),
    ).toBe("desktop-v0.4.17-sso.5");
  });

  it("returns null when the feed has no desktop release", () => {
    expect(
      latestDesktopReleaseTagFromAtom(`
        <feed>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/v0.4.16-sso.1"/>
          </entry>
        </feed>
      `),
    ).toBeNull();
  });
});

describe("configureForkUpdateFeed", () => {
  it("points electron-updater at the selected Release asset folder", async () => {
    const updater = { setFeedURL: vi.fn() };
    const fetchImpl = vi.fn(async () => ({
      ok: true,
      status: 200,
      text: async () => `
        <feed>
          <entry>
            <link rel="alternate" href="https://github.com/coder-zkl1988/multica/releases/tag/desktop-v0.4.17-sso.5"/>
          </entry>
        </feed>
      `,
    })) as unknown as typeof fetch;

    await expect(
      configureForkUpdateFeed(
        updater,
        "coder-zkl1988/multica",
        fetchImpl,
      ),
    ).resolves.toBe("desktop-v0.4.17-sso.5");

    expect(updater.setFeedURL).toHaveBeenCalledWith({
      provider: "generic",
      url: "https://github.com/coder-zkl1988/multica/releases/download/desktop-v0.4.17-sso.5/",
    });
  });

  it("rejects an invalid repository before making a request", async () => {
    const fetchImpl = vi.fn() as unknown as typeof fetch;

    await expect(
      configureForkUpdateFeed(
        { setFeedURL: vi.fn() },
        "https://github.com/example/repo",
        fetchImpl,
      ),
    ).rejects.toThrow("Invalid GitHub repository");
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});

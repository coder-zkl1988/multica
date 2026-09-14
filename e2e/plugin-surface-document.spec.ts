import { expect, test } from "@playwright/test";
import { buildSurfaceFrameDocument } from "../packages/views/plugins/surface-document";

/**
 * Real Chromium coverage for behavior jsdom does not implement: executing a
 * sandboxed srcdoc document, relaying a nested frame handshake, and reporting
 * a synchronous plugin error from the hosted frame.
 */

test.describe("plugin surface document (real Chromium, sandboxed srcdoc)", () => {
  test("a host-authored srcdoc reload is not reported as hostile navigation", async ({ page }) => {
    await page.route("https://plugin-content.example.test/**", async (route) => {
      await route.fulfill({
        contentType: "text/html",
        headers: { "Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'" },
        body: `<!doctype html><script>
          const channel = new MessageChannel();
          parent.postMessage({
            type: "multica:plugin-bridge-connect",
            version: 2,
            challenge: "proof"
          }, "*", [channel.port1]);
        </script>`,
      });
    });
    const wrapper = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "proof",
    });

    await page.setContent(`<script>window.surfaceState = { bridgeCount: 0, navigated: 0 }; addEventListener("message", event => {
      if (event.data?.type === "multica:plugin-bridge-connect" && event.ports[0]) window.surfaceState.bridgeCount += 1;
      if (event.data?.type === "multica:plugin-surface-navigated") window.surfaceState.navigated += 1;
    });</script><iframe id="host" sandbox="allow-scripts allow-same-origin"></iframe>`);
    await page.locator("#host").evaluate((frame, srcdoc) => {
      (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
    }, wrapper);

    await expect.poll(() => page.evaluate(() => (window as unknown as { surfaceState: { bridgeCount: number } }).surfaceState.bridgeCount)).toBe(1);

    await page.locator("#host").evaluate((frame, srcdoc) => {
      (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
    }, wrapper);
    await expect.poll(() => page.evaluate(() => (window as unknown as { surfaceState: { bridgeCount: number } }).surfaceState.bridgeCount)).toBe(2);

    expect(await page.evaluate(() => (window as unknown as { surfaceState: { navigated: number } }).surfaceState.navigated)).toBe(0);
  });

  test("reports a synchronous plugin error from the hosted frame", async ({ page }) => {
    await page.route("https://plugin-content.example.test/**", async (route) => {
      await route.fulfill({
        contentType: "text/html",
        headers: { "Content-Security-Policy": "default-src 'none'; script-src 'unsafe-inline'" },
        body: `<!doctype html><script>
          parent.postMessage({ type: "multica:plugin-surface-error" }, "*");
        </script>`,
      });
    });
    const wrapper = buildSurfaceFrameDocument({
      url: "https://plugin-content.example.test/plugin-surfaces/opaque-token",
      bridgeToken: "proof",
    });

    await page.setContent(`<script>window.surfaceErrors = 0; addEventListener("message", event => {
      if (event.data?.type === "multica:plugin-surface-error") window.surfaceErrors += 1;
    });</script><iframe id="host" sandbox="allow-scripts allow-same-origin"></iframe>`);
    await page.locator("#host").evaluate((frame, srcdoc) => {
      (frame as HTMLIFrameElement).srcdoc = srcdoc as string;
    }, wrapper);

    await expect.poll(() => page.evaluate(() => (window as unknown as { surfaceErrors: number }).surfaceErrors)).toBe(1);
  });
});

import { expect, type Page } from "@playwright/test";
import { TestApiClient } from "./fixtures";

const DEFAULT_E2E_NAME = "E2E User";
const E2E_WORKER = process.env.TEST_PARALLEL_INDEX ?? process.env.TEST_WORKER_INDEX ?? "0";
const E2E_RUN_ID = process.env.E2E_RUN_ID ?? `${Date.now().toString(36)}-${process.pid.toString(36)}`;
const DEFAULT_E2E_EMAIL = `e2e-${E2E_WORKER}-${E2E_RUN_ID}@multica.ai`;
const DEFAULT_E2E_WORKSPACE = `e2e-workspace-${E2E_WORKER}-${E2E_RUN_ID}`;

async function waitForIssuesPage(page: Page) {
  await waitForPageText(page, "New Issue");
  await expect(page.getByRole("button", { name: "New Issue" })).toBeVisible({
    timeout: 15000,
  });
}

export async function waitForPageText(page: Page, text: string, timeout = 30000) {
  await page.waitForFunction(
    (expected) => document.body?.innerText.includes(expected),
    text,
    { timeout },
  );
}

export async function reloadAppPage(page: Page) {
  await page.reload({ waitUntil: "domcontentloaded" });
  await waitForPageText(page, "Issues");
}

const FRONTEND_ORIGIN = new URL(
  process.env.PLAYWRIGHT_BASE_URL ?? process.env.FRONTEND_ORIGIN ?? "http://localhost:3000",
).origin;

export async function authenticatePage(
  page: Page,
  api: TestApiClient,
  workspaceSlug?: string,
) {
  const { token, csrfToken, expiresAt } = api.getBrowserSession();
  const cookies = [
    { name: "multica_auth", value: token, url: FRONTEND_ORIGIN, httpOnly: true, sameSite: "Strict" as const, expires: expiresAt },
    { name: "multica_csrf", value: csrfToken, url: FRONTEND_ORIGIN, sameSite: "Strict" as const, expires: expiresAt },
    { name: "multica_logged_in", value: "1", url: FRONTEND_ORIGIN, sameSite: "Lax" as const, expires: expiresAt },
  ];
  if (workspaceSlug) {
    cookies.push({ name: "last_workspace_slug", value: workspaceSlug, url: FRONTEND_ORIGIN, sameSite: "Lax", expires: expiresAt });
  }
  await page.context().addCookies(cookies);
}

/**
 * Log in as the default E2E user and ensure the workspace exists first.
 * Creates a local SSO test identity, then injects the same HttpOnly auth and
 * CSRF cookies used by the web app.
 *
 * Returns the E2E workspace slug so callers can build workspace-scoped URLs.
 */
export async function loginAsDefault(page: Page): Promise<string> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  const workspace = await api.ensureWorkspace(
    `E2E Workspace ${E2E_WORKER}`,
    DEFAULT_E2E_WORKSPACE,
  );
  await api.markUserOnboarded();

  await page.addInitScript(() => {
    localStorage.setItem("multica:chat:isOpen", "false");
  });
  await authenticatePage(page, api, workspace.slug);
  await page.goto(`/${workspace.slug}/issues`, { waitUntil: "domcontentloaded" });
  await page.waitForURL("**/issues", { timeout: 10000 });
  await waitForIssuesPage(page);
  return workspace.slug;
}

/**
 * Create a TestApiClient logged in as the default E2E user.
 * Call api.cleanup() in afterEach to remove test data created during the test.
 */
export async function createTestApi(): Promise<TestApiClient> {
  const api = new TestApiClient();
  await api.login(DEFAULT_E2E_EMAIL, DEFAULT_E2E_NAME);
  await api.ensureWorkspace(`E2E Workspace ${E2E_WORKER}`, DEFAULT_E2E_WORKSPACE);
  await api.markUserOnboarded();
  return api;
}

export async function preferManualCreateMode(page: Page) {
  await page.evaluate(() => {
    localStorage.setItem(
      "multica_create_mode",
      JSON.stringify({ state: { lastMode: "manual" }, version: 0 }),
    );
  });
  await reloadAppPage(page);
  await waitForIssuesPage(page);
}

export async function openWorkspaceMenu(page: Page) {
  // Click the workspace switcher button (has ChevronDown icon)
  const workspaceButton = page.getByRole("button", { name: /E2E Workspace/ }).first();
  await expect(workspaceButton).toBeVisible({ timeout: 15000 });
  await workspaceButton.click();
  // Wait for dropdown to appear
  await expect(page.locator('[class*="popover"]')).toBeVisible();
}

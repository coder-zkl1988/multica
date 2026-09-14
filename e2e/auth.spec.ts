import { test, expect } from "@playwright/test";
import { loginAsDefault, openWorkspaceMenu } from "./helpers";

const useSySSO = /^(1|true|yes|on)$/i.test(process.env.USE_SY_SSO?.trim() ?? "");

test.describe("Authentication", () => {
  test("login page renders correctly for the configured auth mode", async ({ page }) => {
    await page.goto("/login");

    if (useSySSO) {
      await expect(page.getByText("Sign-in failed", { exact: true })).toBeVisible();
      await expect(page.getByRole("button", { name: "Retry" })).toBeVisible();
      await expect(page.locator('input[placeholder="Email"]')).toHaveCount(0);
      return;
    }

    await expect(page.getByRole("textbox", { name: "Email" })).toBeVisible();
    await expect(page.getByRole("button", { name: "Continue" })).toBeDisabled();
    await expect(page.getByText("Sign-in failed", { exact: true })).toHaveCount(0);
  });

  test("login and redirect to /issues", async ({ page }) => {
    await loginAsDefault(page);

    await expect(page).toHaveURL(/\/issues/);
    await expect(page.getByRole("heading", { name: "Issues" })).toBeVisible();
  });

  test("unauthenticated user is redirected to /login", async ({ page, context }) => {
    await context.clearCookies();

    // Visit a workspace-scoped route; DashboardGuard should redirect to /login.
    // The slug here need not exist — the guard runs before workspace resolution
    // for unauthenticated users.
    await page.goto("/e2e-workspace/issues");
    await page.waitForURL("**/login", { timeout: 10000 });
  });

  test("logout redirects to the configured auth entry", async ({ page }) => {
    await loginAsDefault(page);

    // Open the workspace dropdown menu
    await openWorkspaceMenu(page);

    const logoutResponse = page.waitForResponse(
      (response) =>
        response.url().endsWith("/auth/logout") &&
        response.request().method() === "POST",
    );
    await page.getByRole("menuitem", { name: "Log out" }).click();
    await expect((await logoutResponse).status()).toBe(200);

    if (useSySSO) {
      await page.waitForURL("**/logout", { timeout: 10000 });
      await expect(page).toHaveURL(/\/logout/);
    } else {
      await page.waitForURL("**/login", { timeout: 10000 });
      await expect(page).toHaveURL(/\/login/);
    }
  });
});

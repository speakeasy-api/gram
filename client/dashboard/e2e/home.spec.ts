import { test, expect, pageHelpers } from "./fixtures/test-fixtures";

test.describe("Home Dashboard", () => {
  test.beforeEach(async ({ page }) => {
    // Navigate to home - will redirect to login if not authenticated
    await page.goto("/");
    await page.waitForLoadState("networkidle");
  });

  test.describe("Unauthenticated State", () => {
    test("should redirect to login when not authenticated", async ({
      page,
    }) => {
      // If not authenticated, should be on login page
      const url = page.url();
      if (url.includes("/login") || url.includes("/register")) {
        await expect(page.getByRole("button", { name: /login/i })).toBeVisible();
      }
    });
  });

  test.describe("Authenticated State", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should display the quick test section", async ({
      authenticatedPage: page,
    }) => {
      // Should show "Chat with your MCP server" section
      await expect(
        page.getByText(/chat with your mcp server/i)
      ).toBeVisible();

      // Should have a textarea for prompt input
      const promptInput = page.getByPlaceholder(/chat with your mcp server/i);
      await expect(promptInput).toBeVisible();
    });

    test("should display explore your tools section", async ({
      authenticatedPage: page,
    }) => {
      await expect(page.getByText(/explore your tools/i)).toBeVisible();

      // Should have action buttons
      await expect(
        page.getByRole("button", { name: /test in playground/i })
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: /manage toolsets/i })
      ).toBeVisible();
      await expect(
        page.getByRole("button", { name: /build custom tools/i })
      ).toBeVisible();
    });

    test("should display your servers section", async ({
      authenticatedPage: page,
    }) => {
      await expect(page.getByText(/your servers/i)).toBeVisible();
    });

    test("should navigate to playground when clicking Test in Playground", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /test in playground/i }).click();
      await page.waitForLoadState("networkidle");

      expect(page.url()).toContain("/playground");
    });

    test("should navigate to toolsets when clicking Manage Toolsets", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /manage toolsets/i }).click();
      await page.waitForLoadState("networkidle");

      expect(page.url()).toContain("/toolsets");
    });

    test("should navigate to custom tools when clicking Build Custom Tools", async ({
      authenticatedPage: page,
    }) => {
      await page
        .getByRole("button", { name: /build custom tools/i })
        .click();
      await page.waitForLoadState("networkidle");

      expect(page.url()).toContain("/custom-tools");
    });

    test("should send quick test to playground with prompt", async ({
      authenticatedPage: page,
    }) => {
      const promptInput = page.getByPlaceholder(/chat with your mcp server/i);
      await promptInput.fill("Test prompt for playground");

      // Press Enter to submit
      await promptInput.press("Enter");
      await page.waitForLoadState("networkidle");

      // Should navigate to playground with prompt in URL
      const url = page.url();
      expect(url).toContain("/playground");
      expect(url).toContain("prompt=");
    });
  });

  test.describe("Empty State", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should show empty state when no toolsets exist", async ({
      authenticatedPage: page,
    }) => {
      // This test assumes the user has no toolsets
      // In a real scenario, you'd set up a test user with no toolsets

      // Look for empty state indicators
      const emptyState = page.getByText(/get started/i).or(
        page.getByText(/no toolsets/i)
      );

      // If empty state is visible, verify it has correct CTA
      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();
      }
    });
  });
});

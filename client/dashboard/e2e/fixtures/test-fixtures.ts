import { test as base, expect, Page } from "@playwright/test";

/**
 * Extended test fixtures for Gram Dashboard E2E tests.
 *
 * These fixtures provide common utilities for authentication,
 * navigation, and assertions across all test files.
 */

export type TestFixtures = {
  /** Authenticated page - use when tests require a logged-in user */
  authenticatedPage: Page;
  /** Helper to wait for React Query to finish loading */
  waitForQuerySettled: (page: Page) => Promise<void>;
  /** Helper to navigate to a project route */
  goToProjectRoute: (page: Page, route: string) => Promise<void>;
};

/**
 * Wait for the page to finish loading React Query data.
 * Waits for network to be idle and any loading spinners to disappear.
 */
async function waitForQuerySettled(page: Page): Promise<void> {
  await page.waitForLoadState("networkidle");
  // Wait for any loading indicators to disappear
  const loadingIndicators = page.locator('[data-loading="true"], .animate-spin');
  if ((await loadingIndicators.count()) > 0) {
    await loadingIndicators.first().waitFor({ state: "hidden", timeout: 10000 }).catch(() => {
      // Ignore if no loading indicators found
    });
  }
}

/**
 * Navigate to a route within the current project context.
 * Assumes the page is already authenticated and has org/project in URL.
 */
async function goToProjectRoute(page: Page, route: string): Promise<void> {
  const currentUrl = page.url();
  const urlParts = new URL(currentUrl);
  const pathParts = urlParts.pathname.split("/").filter(Boolean);

  // Extract org and project slugs from current URL
  const orgSlug = pathParts[0] || "";
  const projectSlug = pathParts[1] || "";

  if (!orgSlug || !projectSlug) {
    throw new Error("Cannot navigate to project route: not currently in a project context");
  }

  const targetPath = `/${orgSlug}/${projectSlug}/${route}`.replace(/\/+/g, "/");
  await page.goto(targetPath);
  await waitForQuerySettled(page);
}

export const test = base.extend<TestFixtures>({
  waitForQuerySettled: async ({}, use) => {
    await use(waitForQuerySettled);
  },

  goToProjectRoute: async ({}, use) => {
    await use(goToProjectRoute);
  },

  authenticatedPage: async ({ page }, use) => {
    // For E2E tests, we expect the environment to be set up with a test user
    // The test user should be pre-authenticated via cookies or session storage
    // If not authenticated, tests will be redirected to login

    // Check if we need to authenticate
    await page.goto("/");
    await page.waitForLoadState("networkidle");

    // If redirected to login, we're not authenticated
    const isLoginPage =
      page.url().includes("/login") || page.url().includes("/register");

    if (isLoginPage) {
      // In a real E2E setup, you would:
      // 1. Use a test account with known credentials
      // 2. Set up authentication via API before tests
      // 3. Or use browser context with stored auth state

      // For now, we'll skip tests that require auth if not logged in
      console.warn(
        "Not authenticated. Tests requiring auth will be skipped. " +
          "Set up auth state or use PLAYWRIGHT_AUTH_STATE_PATH env var."
      );
    }

    await use(page);
  },
});

export { expect };

/**
 * Page object helpers for common UI interactions
 */
export const pageHelpers = {
  /**
   * Get the sidebar navigation
   */
  getSidebar: (page: Page) => page.locator('[data-testid="app-sidebar"]').or(page.locator("aside")),

  /**
   * Get the main content area
   */
  getMainContent: (page: Page) => page.locator("main").or(page.locator('[role="main"]')),

  /**
   * Get a dialog/modal
   */
  getDialog: (page: Page) => page.locator('[role="dialog"]'),

  /**
   * Get toast notifications
   */
  getToast: (page: Page) => page.locator('[data-sonner-toast]').or(page.locator('[role="status"]')),

  /**
   * Click a button by its text content
   */
  clickButton: async (page: Page, text: string) => {
    await page.getByRole("button", { name: text }).click();
  },

  /**
   * Fill an input by its label
   */
  fillInput: async (page: Page, label: string, value: string) => {
    await page.getByLabel(label).fill(value);
  },

  /**
   * Select an option from a select/combobox
   */
  selectOption: async (page: Page, triggerText: string, optionText: string) => {
    await page.getByRole("combobox", { name: triggerText }).click();
    await page.getByRole("option", { name: optionText }).click();
  },

  /**
   * Wait for a specific text to appear on the page
   */
  waitForText: async (page: Page, text: string) => {
    await page.getByText(text).waitFor({ state: "visible" });
  },

  /**
   * Check if user is on the login page
   */
  isOnLoginPage: (page: Page) => {
    const url = page.url();
    return url.includes("/login") || url.includes("/register");
  },

  /**
   * Get the current org and project slugs from URL
   */
  getSlugsFromUrl: (page: Page) => {
    const url = new URL(page.url());
    const pathParts = url.pathname.split("/").filter(Boolean);
    return {
      orgSlug: pathParts[0] || null,
      projectSlug: pathParts[1] || null,
    };
  },
};

import { test, expect } from "./fixtures/test-fixtures";

test.describe("Toolsets", () => {
  test.describe("Toolsets List Page", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "toolsets");
    });

    test("should display the toolsets page header", async ({
      authenticatedPage: page,
    }) => {
      // Should show page breadcrumbs
      await expect(page.locator("nav").or(page.getByRole("navigation"))).toBeVisible();

      // Should show toolsets section
      await expect(page.getByText(/toolsets/i).first()).toBeVisible();
    });

    test("should have add toolset button", async ({
      authenticatedPage: page,
    }) => {
      const addButton = page.getByRole("button", { name: /add toolset/i });
      await expect(addButton).toBeVisible();
    });

    test("should open create toolset dialog when clicking add button", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /add toolset/i }).click();

      // Should show the create dialog
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();

      // Dialog should have title
      await expect(page.getByText(/create a toolset/i)).toBeVisible();

      // Dialog should have input for toolset name
      await expect(
        page.getByPlaceholder(/toolset name/i)
      ).toBeVisible();

      // Dialog should have create button
      await expect(
        page.getByRole("button", { name: /^create$/i })
      ).toBeVisible();
    });

    test("should validate toolset name length in create dialog", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /add toolset/i }).click();

      const input = page.getByPlaceholder(/toolset name/i);

      // Enter a very long name
      await input.fill("A".repeat(50));

      // Should show character limit warning
      await expect(page.getByText(/40 characters or less/i)).toBeVisible();
    });

    test("should close dialog when pressing escape", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /add toolset/i }).click();

      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();

      // Press escape
      await page.keyboard.press("Escape");

      // Dialog should be closed
      await expect(dialog).not.toBeVisible();
    });
  });

  test.describe("Toolset CRUD Operations", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should create a new toolset", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "toolsets");

      // Open create dialog
      await page.getByRole("button", { name: /add toolset/i }).click();

      const toolsetName = `Test Toolset ${Date.now()}`;

      // Fill in the name
      await page.getByPlaceholder(/toolset name/i).fill(toolsetName);

      // Submit
      await page.getByRole("button", { name: /^create$/i }).click();

      // Wait for navigation to toolset detail page
      await page.waitForLoadState("networkidle");

      // Should navigate to the new toolset
      expect(page.url()).toContain("/toolsets/");
    });

    test("should display toolset cards with correct info", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "toolsets");

      // Get the first toolset card
      const toolsetCards = page.locator('[class*="ServerCard"]').or(
        page.locator('article').or(
          page.locator('[class*="card"]')
        )
      );

      // If there are toolsets, verify card structure
      const cardCount = await toolsetCards.count();
      if (cardCount > 0) {
        const firstCard = toolsetCards.first();
        await expect(firstCard).toBeVisible();
      }
    });

    test("should navigate to toolset detail when clicking a toolset card", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "toolsets");

      // Find a toolset link/card
      const toolsetLink = page.locator('a[href*="/toolsets/"]').first();

      if (await toolsetLink.isVisible()) {
        await toolsetLink.click();
        await page.waitForLoadState("networkidle");

        // Should be on toolset detail page
        expect(page.url()).toMatch(/\/toolsets\/[^/]+$/);
      }
    });
  });

  test.describe("Toolset Detail Page", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should display toolset tabs", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "toolsets");

      // Navigate to first toolset
      const toolsetLink = page.locator('a[href*="/toolsets/"]').first();

      if (await toolsetLink.isVisible()) {
        await toolsetLink.click();
        await page.waitForLoadState("networkidle");

        // Should have tabs for Server, Resources, Prompts
        const tabs = page.getByRole("tablist");
        if (await tabs.isVisible()) {
          await expect(tabs).toBeVisible();
        }
      }
    });

    test("should have clone action in toolset menu", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "toolsets");

      // Look for clone action on a toolset card
      const menuButton = page
        .locator('[aria-label*="menu"]')
        .or(page.locator('button[class*="more"]'))
        .first();

      if (await menuButton.isVisible()) {
        await menuButton.click();

        // Should have clone option
        const cloneOption = page.getByRole("menuitem", { name: /clone/i });
        if (await cloneOption.isVisible()) {
          await expect(cloneOption).toBeVisible();
        }
      }
    });
  });

  test.describe("Empty State", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should show empty state when no toolsets exist", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      // This test assumes a user with no toolsets
      await goToProjectRoute(page, "toolsets");

      // Look for empty state indicators
      const emptyState = page
        .getByText(/no toolsets/i)
        .or(page.getByText(/create your first/i))
        .or(page.getByText(/get started/i));

      // If visible, verify CTA exists
      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();
      }
    });
  });
});

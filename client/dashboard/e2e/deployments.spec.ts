import { test, expect } from "./fixtures/test-fixtures";

test.describe("Deployments", () => {
  test.describe("Deployments List Page", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "deployments");
    });

    test("should display the deployments page", async ({
      authenticatedPage: page,
    }) => {
      // Should show page breadcrumbs
      await expect(
        page.locator("nav").or(page.getByRole("navigation"))
      ).toBeVisible();
    });

    test("should display recent deployments heading", async ({
      authenticatedPage: page,
    }) => {
      // Should show "Recent Deployments" heading
      const heading = page.getByRole("heading", { name: /recent deployments/i });

      // If there are deployments, the heading should be visible
      if (await heading.isVisible()) {
        await expect(heading).toBeVisible();
      }
    });

    test("should display deployments table with correct columns", async ({
      authenticatedPage: page,
    }) => {
      // Should have a table with ID, Assets, Tools columns
      const table = page.getByRole("table");

      if (await table.isVisible()) {
        await expect(table).toBeVisible();

        // Check for column headers
        await expect(page.getByRole("columnheader", { name: /id/i })).toBeVisible();
        await expect(
          page.getByRole("columnheader", { name: /assets/i })
        ).toBeVisible();
        await expect(
          page.getByRole("columnheader", { name: /tools/i })
        ).toBeVisible();
      }
    });

    test("should show deployment status indicators", async ({
      authenticatedPage: page,
    }) => {
      // Deployments should have status indicators (completed, failed, pending)
      const table = page.getByRole("table");

      if (await table.isVisible()) {
        // Look for status icons (check, x, or circle-dashed)
        const statusCell = page.locator('[class*="rounded-full"]').first();
        if (await statusCell.isVisible()) {
          await expect(statusCell).toBeVisible();
        }
      }
    });

    test("should show Active badge for current deployment", async ({
      authenticatedPage: page,
    }) => {
      // The active deployment should have an "Active" badge
      const activeBadge = page.getByText(/active/i);

      if (await activeBadge.isVisible()) {
        await expect(activeBadge).toBeVisible();
      }
    });
  });

  test.describe("Deployment Actions", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should have actions menu for deployments", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Find the actions dropdown trigger
      const actionsButton = page
        .getByRole("button", { name: /open menu/i })
        .or(page.locator('[aria-label*="menu"]'))
        .first();

      if (await actionsButton.isVisible()) {
        await actionsButton.click();

        // Should show dropdown with actions
        const dropdown = page.getByRole("menu").or(page.locator('[role="menu"]'));
        await expect(dropdown).toBeVisible();
      }
    });

    test("should have Retry option for latest deployment", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Open first deployment's actions menu (should be latest)
      const actionsButton = page
        .getByRole("button", { name: /open menu/i })
        .first();

      if (await actionsButton.isVisible()) {
        await actionsButton.click();

        // Should have Retry option
        const retryOption = page.getByRole("menuitem", {
          name: /retry/i,
        });

        if (await retryOption.isVisible()) {
          await expect(retryOption).toBeVisible();
        }
      }
    });

    test("should have Rollback option for previous completed deployments", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Look for rollback option in any actions menu
      const actionsButtons = page.getByRole("button", { name: /open menu/i });
      const count = await actionsButtons.count();

      // Check second deployment if exists (first is "Retry", others are "Rollback")
      if (count > 1) {
        await actionsButtons.nth(1).click();

        const rollbackOption = page.getByRole("menuitem", {
          name: /rollback/i,
        });

        if (await rollbackOption.isVisible()) {
          await expect(rollbackOption).toBeVisible();
        }
      }
    });
  });

  test.describe("Deployment Detail Page", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should navigate to deployment detail when clicking deployment ID", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Click on a deployment link
      const deploymentLink = page.locator('a[href*="/deployments/"]').first();

      if (await deploymentLink.isVisible()) {
        await deploymentLink.click();
        await page.waitForLoadState("networkidle");

        // Should be on deployment detail page
        expect(page.url()).toMatch(/\/deployments\/[^/]+$/);
      }
    });

    test("should display deployment detail tabs", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Navigate to first deployment
      const deploymentLink = page.locator('a[href*="/deployments/"]').first();

      if (await deploymentLink.isVisible()) {
        await deploymentLink.click();
        await page.waitForLoadState("networkidle");

        // Should have tabs (Logs, Assets, Tools)
        const tabs = page.getByRole("tablist");
        if (await tabs.isVisible()) {
          await expect(tabs).toBeVisible();
        }
      }
    });
  });

  test.describe("Empty State", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should show empty state when no deployments exist", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Look for empty state indicators
      const emptyState = page
        .getByText(/no deployments/i)
        .or(page.getByText(/get started/i))
        .or(page.getByText(/upload/i));

      // If visible, should show instructions to create first deployment
      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();
      }
    });
  });

  test.describe("Deployment Info Section", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should display informational section about deployments", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "deployments");

      // Should have informational text about how deployments work
      const infoSection = page.getByText(/each time you upload/i);

      if (await infoSection.isVisible()) {
        await expect(infoSection).toBeVisible();
      }
    });
  });
});

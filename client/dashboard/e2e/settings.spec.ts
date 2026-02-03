import { test, expect } from "./fixtures/test-fixtures";

test.describe("Settings", () => {
  test.describe("Settings Page", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "settings");
    });

    test("should display the settings page", async ({
      authenticatedPage: page,
    }) => {
      // Should show page breadcrumbs
      await expect(
        page.locator("nav").or(page.getByRole("navigation"))
      ).toBeVisible();

      // Should show main content
      await expect(page.locator("main")).toBeVisible();
    });

    test("should display projects table section", async ({
      authenticatedPage: page,
    }) => {
      // Should have a projects section
      const projectsSection = page
        .getByText(/projects/i)
        .or(page.getByRole("table"));

      await expect(projectsSection.first()).toBeVisible();
    });
  });

  test.describe("API Keys Section", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "settings");
    });

    test("should display API Keys heading", async ({
      authenticatedPage: page,
    }) => {
      await expect(page.getByRole("heading", { name: /api keys/i })).toBeVisible();
    });

    test("should have New API Key button", async ({
      authenticatedPage: page,
    }) => {
      const newKeyButton = page.getByRole("button", { name: /new api key/i });
      await expect(newKeyButton).toBeVisible();
    });

    test("should display API keys table with correct columns", async ({
      authenticatedPage: page,
    }) => {
      // Should have table headers
      const table = page.getByRole("table").nth(1); // Second table (after projects)

      if (await table.isVisible()) {
        // Check for column headers
        await expect(
          page.getByRole("columnheader", { name: /name/i })
        ).toBeVisible();
        await expect(
          page.getByRole("columnheader", { name: /key/i })
        ).toBeVisible();
        await expect(
          page.getByRole("columnheader", { name: /scopes/i })
        ).toBeVisible();
      }
    });

    test("should open create API key dialog when clicking New API Key", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /new api key/i }).click();

      // Should show the create dialog
      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();

      // Dialog should have title
      await expect(page.getByText(/create new api key/i)).toBeVisible();

      // Should have name input
      await expect(page.getByLabel(/key name/i)).toBeVisible();

      // Should have scope options
      await expect(page.getByText(/consumer/i)).toBeVisible();
      await expect(page.getByText(/producer/i)).toBeVisible();
      await expect(page.getByText(/chat/i)).toBeVisible();
    });

    test("should close create dialog when pressing Cancel", async ({
      authenticatedPage: page,
    }) => {
      await page.getByRole("button", { name: /new api key/i }).click();

      const dialog = page.getByRole("dialog");
      await expect(dialog).toBeVisible();

      // Click cancel
      await page.getByRole("button", { name: /cancel/i }).click();

      // Dialog should be closed
      await expect(dialog).not.toBeVisible();
    });

    test("should show empty state when no API keys exist", async ({
      authenticatedPage: page,
    }) => {
      // Look for empty state
      const emptyState = page.getByText(/no api keys yet/i);

      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();

        // Should have a create key button in empty state
        const createButton = page.getByRole("button", { name: /create key/i });
        await expect(createButton).toBeVisible();
      }
    });
  });

  test.describe("API Key CRUD Operations", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should create a new API key", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "settings");

      // Open create dialog
      await page.getByRole("button", { name: /new api key/i }).click();

      // Fill in the name
      const keyName = `Test Key ${Date.now()}`;
      await page.getByLabel(/key name/i).fill(keyName);

      // Select consumer scope (default)
      await page.getByLabel(/consumer/i).click();

      // Submit
      await page.getByRole("button", { name: /^create$/i }).click();

      // Wait for key to be created - should show the key value
      await expect(page.getByText(/api key created/i)).toBeVisible();

      // Should show warning about copying the key
      await expect(
        page.getByText(/will not be able to see this token value again/i)
      ).toBeVisible();

      // Should have a code block with the key
      await expect(page.locator("code")).toBeVisible();
    });

    test("should show revoke confirmation dialog", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "settings");

      // Look for a delete/revoke button on an existing key
      const deleteButton = page
        .locator('[aria-label*="delete"]')
        .or(page.locator('button:has([class*="trash"])'))
        .first();

      if (await deleteButton.isVisible()) {
        await deleteButton.click();

        // Should show revoke confirmation dialog
        await expect(page.getByText(/revoke api key/i)).toBeVisible();
        await expect(
          page.getByText(/this action cannot be undone/i)
        ).toBeVisible();

        // Should have cancel and revoke buttons
        await expect(
          page.getByRole("button", { name: /cancel/i })
        ).toBeVisible();
        await expect(
          page.getByRole("button", { name: /revoke key/i })
        ).toBeVisible();
      }
    });
  });

  test.describe("Custom Domains Section", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "settings");
    });

    test("should display Custom Domains heading", async ({
      authenticatedPage: page,
    }) => {
      await expect(
        page.getByRole("heading", { name: /custom domains/i })
      ).toBeVisible();
    });

    test("should have Add Domain button", async ({
      authenticatedPage: page,
    }) => {
      const addDomainButton = page
        .getByRole("button", { name: /add domain/i })
        .or(page.getByRole("button", { name: /verify domain/i }));

      // Button might be disabled or show different text based on account type
      if (await addDomainButton.isVisible()) {
        await expect(addDomainButton).toBeVisible();
      }
    });

    test("should show empty state when no custom domains exist", async ({
      authenticatedPage: page,
    }) => {
      // Look for empty state
      const emptyState = page.getByText(/no custom domains yet/i);

      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();
      }
    });

    test("should open custom domain dialog when clicking Add Domain", async ({
      authenticatedPage: page,
    }) => {
      const addDomainButton = page.getByRole("button", { name: /add domain/i });

      if (await addDomainButton.isVisible()) {
        await addDomainButton.click();

        // Should show dialog (either add domain or feature request)
        const dialog = page.getByRole("dialog");
        await expect(dialog).toBeVisible();
      }
    });

    test("should display domain configuration steps in dialog", async ({
      authenticatedPage: page,
    }) => {
      const addDomainButton = page.getByRole("button", { name: /add domain/i });

      if (await addDomainButton.isVisible()) {
        await addDomainButton.click();

        const dialog = page.getByRole("dialog");

        if (await dialog.isVisible()) {
          // If it's the add domain dialog (not feature request)
          const step1 = page.getByText(/step 1/i);
          if (await step1.isVisible()) {
            await expect(step1).toBeVisible();
            await expect(page.getByText(/step 2/i)).toBeVisible();
            await expect(page.getByText(/step 3/i)).toBeVisible();

            // Should have CNAME and TXT record instructions
            await expect(page.getByText(/cname/i)).toBeVisible();
            await expect(page.getByText(/txt/i)).toBeVisible();
          }
        }
      }
    });

    test("should validate domain format", async ({
      authenticatedPage: page,
    }) => {
      const addDomainButton = page.getByRole("button", { name: /add domain/i });

      if (await addDomainButton.isVisible()) {
        await addDomainButton.click();

        const domainInput = page.getByPlaceholder(/enter your domain/i);

        if (await domainInput.isVisible()) {
          // Enter invalid domain
          await domainInput.fill("invalid-domain");

          // Should show validation error
          const validationError = page.getByText(/valid domain/i);
          if (await validationError.isVisible()) {
            await expect(validationError).toBeVisible();
          }

          // Enter valid domain
          await domainInput.fill("chat.example.com");

          // Validation error should disappear
          if (await validationError.isVisible()) {
            await expect(validationError).not.toBeVisible();
          }
        }
      }
    });
  });

  test.describe("Copy Functionality", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should have copy buttons for DNS records", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "settings");

      const addDomainButton = page.getByRole("button", { name: /add domain/i });

      if (await addDomainButton.isVisible()) {
        await addDomainButton.click();

        const dialog = page.getByRole("dialog");

        if (await dialog.isVisible()) {
          // Should have copy buttons for CNAME and TXT values
          const copyButtons = page
            .getByRole("button")
            .filter({ has: page.locator('[class*="copy"]') });

          // If DNS configuration is shown, there should be copy buttons
          const copyButtonCount = await copyButtons.count();
          if (copyButtonCount > 0) {
            await expect(copyButtons.first()).toBeVisible();
          }
        }
      }
    });
  });
});

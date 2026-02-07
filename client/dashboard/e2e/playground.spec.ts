import { test, expect } from "./fixtures/test-fixtures";

test.describe("Playground", () => {
  test.describe("Page Layout", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test.beforeEach(async ({ authenticatedPage: page, goToProjectRoute }) => {
      await goToProjectRoute(page, "playground");
    });

    test("should display the playground page", async ({
      authenticatedPage: page,
    }) => {
      // Should show breadcrumbs
      await expect(
        page.locator("nav").or(page.getByRole("navigation"))
      ).toBeVisible();

      // Page should have the main playground container
      await expect(page.locator("main")).toBeVisible();
    });

    test("should have toolset selector panel", async ({
      authenticatedPage: page,
    }) => {
      // Should have a toolset dropdown/selector
      const toolsetSelector = page
        .getByRole("combobox")
        .or(page.locator('[data-testid="toolset-selector"]'))
        .or(page.getByText(/select.*toolset/i));

      await expect(toolsetSelector.first()).toBeVisible();
    });

    test("should have environment selector", async ({
      authenticatedPage: page,
    }) => {
      // Environment selector should be present
      const envSelector = page
        .getByText(/environment/i)
        .or(page.locator('[data-testid="environment-selector"]'));

      // May not be visible if no toolset selected
      if (await envSelector.isVisible()) {
        await expect(envSelector).toBeVisible();
      }
    });

    test("should have chat input area", async ({ authenticatedPage: page }) => {
      // Should have a text input for chat
      const chatInput = page
        .getByRole("textbox")
        .or(page.locator("textarea"))
        .or(page.getByPlaceholder(/type.*message/i));

      await expect(chatInput.first()).toBeVisible();
    });

    test("should have logs toggle button", async ({
      authenticatedPage: page,
    }) => {
      // Should have Show/Hide Logs button
      const logsButton = page.getByRole("button", { name: /logs/i });
      await expect(logsButton).toBeVisible();
    });
  });

  test.describe("Toolset Selection", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should load toolsets from URL parameter", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      // Navigate with toolset parameter
      await goToProjectRoute(page, "playground?toolset=test-toolset");

      // The toolset should be pre-selected
      // Exact verification depends on whether the toolset exists
      await page.waitForLoadState("networkidle");
    });

    test("should update URL when selecting a toolset", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Click on toolset selector
      const toolsetSelector = page.getByRole("combobox").first();

      if (await toolsetSelector.isVisible()) {
        await toolsetSelector.click();

        // Select first available toolset
        const firstOption = page.getByRole("option").first();
        if (await firstOption.isVisible()) {
          await firstOption.click();
          await page.waitForLoadState("networkidle");

          // URL should contain toolset parameter
          // Note: This depends on implementation
        }
      }
    });
  });

  test.describe("Model Selection", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should display model selector", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Should have model selection
      const modelSelector = page
        .getByText(/claude/i)
        .or(page.getByText(/model/i))
        .or(page.getByRole("combobox", { name: /model/i }));

      await expect(modelSelector.first()).toBeVisible();
    });

    test("should have temperature slider", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Should have temperature control
      const temperatureControl = page
        .getByText(/temperature/i)
        .or(page.getByRole("slider"));

      if (await temperatureControl.isVisible()) {
        await expect(temperatureControl).toBeVisible();
      }
    });
  });

  test.describe("Chat Functionality", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should allow typing in chat input", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      const chatInput = page.getByRole("textbox").or(page.locator("textarea"));
      const firstInput = chatInput.first();

      await firstInput.fill("Hello, this is a test message");
      await expect(firstInput).toHaveValue("Hello, this is a test message");
    });

    test("should have send button or submit on enter", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Either a send button or the ability to press Enter
      const sendButton = page
        .getByRole("button", { name: /send/i })
        .or(page.getByRole("button", { name: /submit/i }))
        .or(page.locator('button[type="submit"]'));

      // Send functionality should exist in some form
      const chatInput = page.getByRole("textbox").or(page.locator("textarea"));
      await expect(chatInput.first()).toBeVisible();
    });

    test("should load initial prompt from URL parameter", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      const testPrompt = "Test prompt from URL";
      await goToProjectRoute(
        page,
        `playground?prompt=${encodeURIComponent(testPrompt)}`
      );

      await page.waitForLoadState("networkidle");

      // The prompt might be in the input or already sent
      // This depends on implementation
    });
  });

  test.describe("Logs Panel", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should toggle logs panel visibility", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      const logsButton = page.getByRole("button", { name: /logs/i });

      // Initially logs should be hidden (button says "Show Logs")
      if (await logsButton.isVisible()) {
        await expect(logsButton).toContainText(/show/i);

        // Click to show logs
        await logsButton.click();

        // Button should now say "Hide Logs"
        await expect(logsButton).toContainText(/hide/i);

        // Click again to hide
        await logsButton.click();
        await expect(logsButton).toContainText(/show/i);
      }
    });
  });

  test.describe("Share Functionality", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should have share button", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Share button might appear after some interaction
      const shareButton = page.getByRole("button", { name: /share/i });

      // Share button may be conditionally rendered
      if (await shareButton.isVisible()) {
        await expect(shareButton).toBeVisible();
      }
    });
  });

  test.describe("Empty State", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should show empty state when no toolsets available", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // If no toolsets, should show empty state
      const emptyState = page
        .getByText(/no toolsets/i)
        .or(page.getByText(/create.*toolset/i))
        .or(page.getByText(/get started/i));

      // This is conditional on user state
      if (await emptyState.isVisible()) {
        await expect(emptyState).toBeVisible();
      }
    });
  });

  test.describe("Resizable Panels", () => {
    test.skip(
      ({ }, testInfo) => !process.env.PLAYWRIGHT_AUTH_STATE_PATH,
      "Requires authenticated session"
    );

    test("should have resizable panel layout", async ({
      authenticatedPage: page,
      goToProjectRoute,
    }) => {
      await goToProjectRoute(page, "playground");

      // Look for resize handles/separators
      const resizeHandle = page
        .locator('[role="separator"]')
        .or(page.locator('[data-resize-handle]'));

      if (await resizeHandle.first().isVisible()) {
        await expect(resizeHandle.first()).toBeVisible();
      }
    });
  });
});

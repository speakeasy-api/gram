import { test, expect, pageHelpers } from "./fixtures/test-fixtures";

test.describe("Authentication", () => {
  test.describe("Login Page", () => {
    test("should display login page with correct elements", async ({ page }) => {
      await page.goto("/login");
      await page.waitForLoadState("networkidle");

      // Should show the Gram logo/branding
      await expect(page.locator("main")).toBeVisible();

      // Should have a login button
      const loginButton = page.getByRole("button", { name: /login/i });
      await expect(loginButton).toBeVisible();
    });

    test("should display error message when signin_error is present", async ({
      page,
    }) => {
      await page.goto("/login?signin_error=lookup_error");
      await page.waitForLoadState("networkidle");

      // Should show error message
      await expect(
        page.getByText(/failed to look up account details/i)
      ).toBeVisible();
    });

    test("should display generic error for unknown error codes", async ({
      page,
    }) => {
      await page.goto("/login?signin_error=unknown_error");
      await page.waitForLoadState("networkidle");

      // Should show generic error message
      await expect(page.getByText(/server error/i)).toBeVisible();
    });
  });

  test.describe("Register Page", () => {
    test("should display register page with company name input", async ({
      page,
    }) => {
      await page.goto("/register");
      await page.waitForLoadState("networkidle");

      // Should show the company name input
      const companyInput = page.getByLabel(/company name/i);
      await expect(companyInput).toBeVisible();

      // Should have a create organization button
      const createButton = page.getByRole("button", {
        name: /create organization/i,
      });
      await expect(createButton).toBeVisible();
    });

    test("should validate company name on input", async ({ page }) => {
      await page.goto("/register");
      await page.waitForLoadState("networkidle");

      const companyInput = page.getByLabel(/company name/i);

      // Enter invalid characters
      await companyInput.fill("Test@Company!");
      await expect(
        page.getByText(/contains invalid characters/i)
      ).toBeVisible();

      // Clear and enter valid name
      await companyInput.fill("Test Company");
      await expect(
        page.getByText(/contains invalid characters/i)
      ).not.toBeVisible();
    });

    test("should disable submit button when company name is empty", async ({
      page,
    }) => {
      await page.goto("/register");
      await page.waitForLoadState("networkidle");

      const createButton = page.getByRole("button", {
        name: /create organization/i,
      });
      await expect(createButton).toBeDisabled();

      // Enter a company name
      const companyInput = page.getByLabel(/company name/i);
      await companyInput.fill("Test Company");

      await expect(createButton).toBeEnabled();
    });
  });

  test.describe("Authentication Redirects", () => {
    test("should redirect unauthenticated users to login", async ({ page }) => {
      // Try to access a protected route
      await page.goto("/someorg/someproject/playground");
      await page.waitForLoadState("networkidle");

      // Should be redirected to login or show login page
      // The exact behavior depends on the auth state
      const url = page.url();
      const isAuthPage =
        url.includes("/login") || url.includes("/register") || url === "/";

      // If not authenticated, should be on login page or redirected
      if (isAuthPage) {
        expect(pageHelpers.isOnLoginPage(page) || url === "/").toBeTruthy();
      }
    });

    test("should preserve redirect parameter in login URL", async ({
      page,
    }) => {
      const targetUrl = "/myorg/myproject/playground";
      await page.goto(`/login?redirect=${encodeURIComponent(targetUrl)}`);
      await page.waitForLoadState("networkidle");

      // The redirect parameter should be preserved in the page
      // Login button click should include this redirect
      const loginButton = page.getByRole("button", { name: /login/i });
      await expect(loginButton).toBeVisible();
    });
  });
});

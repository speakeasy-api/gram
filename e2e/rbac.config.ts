import { defineConfig } from "@playwright/test";

// Authorization end-to-end suite: proves who may and may not do what, against
// a running local stack, so a change to how authorization is evaluated can show
// that every outcome is unchanged. See rbac/README.md.

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is not set. Run this suite through `mise run test:rbac-e2e`, " +
      "which loads the worktree's mise env.",
  );
}

export default defineConfig({
  testDir: "./rbac",
  globalSetup: "./rbac/global-setup.ts",
  // Personas share one organization and the dev identity provider has a single
  // "current user", so the suite runs serially.
  workers: 1,
  fullyParallel: false,
  // Above the dashboard login journey's own two-minute wait.
  timeout: 180_000,
  expect: { timeout: 15_000 },
  retries: 0,
  reporter: process.env["CI"]
    ? [
        ["github"],
        ["list"],
        ["html", { open: "never", outputFolder: "./.playwright/rbac-report" }],
      ]
    : [
        ["list"],
        ["html", { open: "never", outputFolder: "./.playwright/rbac-report" }],
      ],
  outputDir: "./.playwright/rbac-results",
  use: {
    baseURL: siteURL,
    ignoreHTTPSErrors: true,
    // Traces record authenticated traffic, so CI, whose report is a public
    // artifact, keeps none.
    trace: process.env["CI"] ? "off" : "retain-on-failure",
    screenshot: "only-on-failure",
    // For machines whose preinstalled Chromium is not the build the pinned
    // runner expects; CI installs the matching build instead.
    ...(process.env["RBAC_E2E_CHROMIUM_PATH"]
      ? {
          launchOptions: {
            executablePath: process.env["RBAC_E2E_CHROMIUM_PATH"],
          },
        }
      : {}),
  },
  projects: [
    { name: "contract", testMatch: "contract/**/*.spec.ts" },
    { name: "ui", testMatch: "ui/**/*.spec.ts" },
  ],
});

import { defineConfig } from "@playwright/test";

// System Google Chrome, sandbox left on. `playwright install` is not used:
// its Chromium build lags the image browser and its `--with-deps` apt step
// is the slow part of client-test. In CI, Chrome is the copy already on the
// Blacksmith Ubuntu 24.04 image (the same image as GitHub's ubuntu-24.04).
// New image builds pick up Chrome stable; this suite does not pin or download
// a browser. https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
export default defineConfig({
  testDir: "./tests",
  testMatch: ["registry-editor.spec.ts", "admin-layout.spec.ts"],
  workers: 1,
  outputDir: "./.playwright",
  timeout: 30_000,
  expect: { timeout: 20_000 },
  use: {
    channel: "chrome",
    launchOptions: {
      // Playwright's test runner omits the sandbox unless this is set. The
      // image Chrome is covered by its AppArmor profile, so leave it on.
      chromiumSandbox: true,
    },
  },
  webServer: {
    command: "aube run -F admin test:browser:serve",
    url: "http://127.0.0.1:4179/test/registry-editor.html",
    timeout: 120_000,
    reuseExistingServer: false,
  },
});

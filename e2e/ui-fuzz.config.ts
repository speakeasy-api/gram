import { defineConfig } from "@playwright/test";

// Jev-judged adversarial UI states against a running stack. Serial and
// single-worker: every case logs in and navigates the same stack, and the
// per-run record directory is shared state.

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is not set. Run this suite through `mise run test:ui-fuzz`, " +
      "which loads the worktree's mise env.",
  );
}

export default defineConfig({
  testDir: "./tests",
  testMatch: "ui-fuzz.spec.ts",
  workers: 1,
  fullyParallel: false,
  timeout: 5 * 60 * 1000,
  expect: { timeout: 20_000 },
  // A retry would ask Jev again and record the case twice.
  retries: 0,
  reporter: process.env["CI"] ? [["github"], ["list"]] : [["list"]],
  outputDir: "./.playwright",
  use: {
    baseURL: siteURL,
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});

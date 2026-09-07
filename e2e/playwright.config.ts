import { defineConfig } from "@playwright/test";

// Browser tests for the worktree lifecycle: pause the stack, hit its URL, and
// resume it from the page the parker serves (see `mise run park`). Everything
// here is deliberately serial and single-worker -- the subject under test is
// one worktree's containers, which is global mutable state.

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is not set. Run this suite through `mise run test:wake-stack`, " +
      "which loads the worktree's mise env.",
  );
}

export default defineConfig({
  testDir: "./tests",
  workers: 1,
  fullyParallel: false,
  // A wake starts containers, waits for Postgres and ClickHouse, then builds
  // and starts the Go server -- minutes on a cold worktree, and the login that
  // follows needs the server to be answering.
  timeout: 12 * 60 * 1000,
  expect: { timeout: 20_000 },
  // Every retry would pay the pause/wake cost again, and a flake here is
  // usually a real stack problem worth reading the trace for.
  retries: 0,
  reporter: [["list"]],
  outputDir: "./.playwright",
  use: {
    baseURL: siteURL,
    // vite serves the worktree's self-signed local cert, and the parker serves
    // the same one so the origin does not change across the handover.
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    video: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});

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
  // Has to sit above every wait the test sets for itself -- the lock wait, the
  // exec timeout on a `mise` task that may run a full boot, the wait for the
  // dashboard to answer. Otherwise this cap binds first and a slow-but-working
  // run dies with a generic test timeout instead of the specific diagnostic the
  // step was written to produce. A healthy run takes about three minutes.
  timeout: 30 * 60 * 1000,
  expect: { timeout: 20_000 },
  // Every retry would pay the pause/wake cost again, and a flake here is
  // usually a real stack problem worth reading the trace for.
  retries: 0,
  // The GitHub reporter annotates the failing line in the PR's Files tab; the
  // list reporter keeps the job log readable on its own.
  reporter: process.env["CI"] ? [["github"], ["list"]] : [["list"]],
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

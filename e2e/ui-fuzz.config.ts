import { defineConfig } from "@playwright/test";

// Jev-judged adversarial UI states against a running stack.

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is not set. Run this suite through `mise run test:ui-fuzz`, " +
      "which loads the worktree's mise env.",
  );
}

export default defineConfig({
  testDir: "./tests",
  // The recorder and the fuzz step are separate invocations (see
  // .mise-tasks/test/ui-fuzz.sh): the fuzz spec generates its cases from the
  // recording at load time, so it cannot run in the same process.
  testMatch: ["ui-fuzz-record.spec.ts", "ui-fuzz.spec.ts"],
  // Each worker logs in once and every case is one page load plus one
  // judgement, so a few workers cut the wall clock without crowding the
  // stack or the judge.
  workers: process.env["CI"] ? 4 : 2,
  fullyParallel: true,
  timeout: 3 * 60 * 1000,
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

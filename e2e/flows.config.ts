import { defineConfig } from "@playwright/test";

// Flow checks: can a user still finish each core product flow? See
// flows/README.md. Separate from playwright.config.ts, which drives the
// worktree pause/resume lifecycle and must run alone.

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is not set. Run flows through `mise run test:flows`, " +
      "which loads the worktree's mise env.",
  );
}

export default defineConfig({
  testDir: "./flows",
  testMatch: "**/flow.spec.ts",
  // Flows create their own uniquely named data, so they can share a stack.
  fullyParallel: true,
  timeout: 3 * 60 * 1000,
  expect: { timeout: 15_000 },
  // A retry would hide exactly the drift this suite exists to catch. Flakes go
  // to the heal agent, which re-walks the story and says which it was.
  retries: 0,
  reporter: [
    ["list"],
    ["json", { outputFile: ".flows/results.json" }],
    ...(process.env["CI"] ? ([["github"]] as const) : []),
  ],
  outputDir: "./.flows/artifacts",
  use: {
    baseURL: siteURL,
    ignoreHTTPSErrors: true,
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});

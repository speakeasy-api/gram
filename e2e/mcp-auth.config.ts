import { defineConfig } from "@playwright/test";

const siteURL = process.env["GRAM_SITE_URL"];
if (!siteURL) {
  throw new Error(
    "GRAM_SITE_URL is required. Run mise run test:mcp-auth from a local worktree.",
  );
}
const loopback = new Set(["localhost", "127.0.0.1", "[::1]"]);
for (const value of [
  siteURL,
  process.env["GRAM_SERVER_URL"],
  process.env["GRAM_ADMIN_URL"] ?? process.env["GRAM_ADMIN_SERVER_URL"],
]) {
  if (!value) continue;
  const url = new URL(value);
  if (
    !loopback.has(url.hostname) ||
    url.username ||
    url.password ||
    !["http:", "https:"].includes(url.protocol)
  ) {
    throw new Error(
      "The auth fixture suite only administers loopback dashboards, never production.",
    );
  }
}
process.env["GRAM_ADMIN_URL"] ??= process.env["GRAM_ADMIN_SERVER_URL"];

export default defineConfig({
  testDir: "./mcp-auth",
  testMatch: "**/*.spec.ts",
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 300_000,
  globalTimeout: 1_800_000,
  expect: { timeout: 20_000 },
  outputDir: "./.playwright/mcp-auth",
  reporter: [
    ["list"],
    ["json", { outputFile: ".playwright/mcp-auth/results.json" }],
  ],
  use: {
    baseURL: siteURL,
    channel: "chrome",
    launchOptions: { chromiumSandbox: true },
    ignoreHTTPSErrors: true,
    actionTimeout: 20_000,
    navigationTimeout: 30_000,
    viewport: { width: 1440, height: 1000 },
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
});

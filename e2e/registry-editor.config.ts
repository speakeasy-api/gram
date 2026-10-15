import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  testMatch: "registry-editor.spec.ts",
  workers: 1,
  outputDir: "./.playwright",
  timeout: 30_000,
  expect: { timeout: 20_000 },
  webServer: {
    command: "aube run -F admin test:browser:serve",
    url: "http://127.0.0.1:4179/test/registry-editor.html",
    timeout: 120_000,
    reuseExistingServer: false,
  },
});

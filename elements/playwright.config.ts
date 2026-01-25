import { defineConfig, devices } from '@playwright/test'

/**
 * Playwright configuration for Gram Elements Storybook tests.
 *
 * These tests run against Storybook with MSW (Mock Service Worker) enabled,
 * meaning all LLM API calls are mocked - there are NO real API costs.
 *
 * Run tests: pnpm test:e2e
 * Run with UI: pnpm test:e2e --ui
 */
export default defineConfig({
  testDir: './e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  workers: process.env.CI ? 1 : undefined,
  reporter: process.env.CI ? 'github' : 'html',
  timeout: 30000,

  use: {
    // Base URL for Storybook
    baseURL: 'http://localhost:6006',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Start Storybook before running tests (with MSW enabled via STORYBOOK_CHROMATIC=true)
  webServer: {
    command: 'STORYBOOK_CHROMATIC=true pnpm storybook --ci',
    url: 'http://localhost:6006',
    reuseExistingServer: !process.env.CI,
    timeout: 120000,
  },
})

import path from "node:path";
import react from "@vitejs/plugin-react";
import { configDefaults, defineConfig } from "vitest/config";
import { withoutLocalDevSlot } from "./dev-slot-plugin.ts";

// Worker threads keep the main thread's time zone, so process.env.TZ never
// takes effect there. These files pin a zone and stay on the forks pool.
const timezoneProcessTests = [
  "src/components/billing/payg-plan-section.test.tsx",
  "src/hooks/useSessionDateRange.test.ts",
  "src/pages/demo/upgrade-gate-copy.test.ts",
];

export default defineConfig({
  plugins: [withoutLocalDevSlot(), react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
      "@gram/client": path.resolve(import.meta.dirname, "./src/sdk/src"),
    },
    conditions: ["source"],
  },
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(""),
    __GRAM_GIT_SHA__: JSON.stringify(""),
    __GRAM_API_URL__: JSON.stringify(""),
    __GRAM_DEV_WORKTREE__: JSON.stringify(""),
    __GRAM_DEV_BRANCH__: JSON.stringify(""),
    __GRAM_DEV_BRANCH_EVENT__: JSON.stringify(""),
    __GRAM_DEV_BRANCH_ASK__: JSON.stringify(""),
  },
  test: {
    pool: "threads",
    fsModuleCache: true,
    environment: "happy-dom",
    setupFiles: ["./src/test/setup.ts"],
    projects: [
      {
        test: {
          name: "threads",
          exclude: [...configDefaults.exclude, ...timezoneProcessTests],
        },
      },
      {
        test: {
          name: "forks",
          pool: "forks",
          include: [...timezoneProcessTests],
        },
      },
    ],
  },
});

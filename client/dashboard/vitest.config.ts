import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
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
  },
  test: {
    environment: "happy-dom",
  },
});

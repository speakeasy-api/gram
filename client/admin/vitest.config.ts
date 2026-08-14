import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // vite.config.ts injects this too. Without it every test file that reaches
  // impersonationUrl() dies with a ReferenceError. Tests get a fixed fake
  // origin rather than "": an empty base is the "not configured" case, and
  // pinning a real one lets a test assert the whole URL.
  define: {
    __GRAM_APP_URL__: JSON.stringify("https://app.gram.test"),
  },
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    environment: "happy-dom",
  },
});

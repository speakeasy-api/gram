import path from "node:path";
import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: [
      { find: "@", replacement: path.resolve(__dirname, "./src") },
      // Redirect all lucide-react imports (including subpath like /dynamicIconImports)
      // to the dashboard's hoisted copy so moonshine resolves correctly.
      {
        find: /^lucide-react(\/.*)?$/,
        replacement:
          path.resolve(__dirname, "node_modules/lucide-react") + "$1",
      },
    ],
  },
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(""),
    __GRAM_GIT_SHA__: JSON.stringify(""),
  },
  test: {
    environment: "happy-dom",
    setupFiles: ["./src/test-setup.ts"],
    server: {
      deps: {
        // Pre-bundle moonshine + lucide-react so ESM subpath imports resolve
        inline: [/@speakeasy-api\/moonshine/, /lucide-react/],
      },
    },
  },
});

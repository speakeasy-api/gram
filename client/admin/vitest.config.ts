import react from "@vitejs/plugin-react";
import { tanstackRouter } from "@tanstack/router-plugin/vite";
import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // A real origin, not "": empty is the not-configured case.
  define: {
    __GRAM_APP_URL__: JSON.stringify("https://app.gram.test"),
  },
  plugins: [
    // Match production route splitting so tests load only the pages they visit.
    tanstackRouter({
      target: "react",
      autoCodeSplitting: true,
      enableRouteGeneration: false,
    }),
    react(),
  ],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
      "@gram/admin-client": path.resolve(import.meta.dirname, "./src/sdk/src"),
    },
  },
  test: {
    // Component and browser-behavior tests. DOM-independent files opt into
    // Node with `// @vitest-environment node` on the first line.
    environment: "happy-dom",
  },
});

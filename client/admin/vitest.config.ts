import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // A real origin, not "": empty is the not-configured case.
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

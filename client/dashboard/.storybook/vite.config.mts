import path from "node:path";
import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// Minimal vite config for Storybook. The app's root vite.config.ts is not
// used here: it demands GRAM_* env vars, dev HTTPS certs, and an API proxy —
// none of which apply to rendering components in isolation.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "../src"),
    },
  },
  define: {
    __GRAM_SERVER_URL__: JSON.stringify("http://localhost:8080"),
    __PLAYGROUND_PROXY_URL__: JSON.stringify(undefined),
    __GRAM_GIT_SHA__: JSON.stringify("storybook"),
  },
});

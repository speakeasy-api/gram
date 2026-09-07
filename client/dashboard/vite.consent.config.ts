import path from "node:path";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Standalone build for the consent tool-access island. The output is embedded
// in the Go server binary (go:embed), so it must be a deterministic,
// self-contained IIFE with fixed file names — no code splitting, no hashes, no
// sourcemaps, and none of the dashboard entry's CDN/chunking behavior.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  publicDir: false,
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(""),
    __PLAYGROUND_PROXY_URL__: "undefined",
    __GRAM_GIT_SHA__: JSON.stringify(""),
    __GRAM_API_URL__: JSON.stringify(""),
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      "@gram/client": path.resolve(__dirname, "./src/sdk/src"),
    },
  },
  build: {
    outDir: path.resolve(__dirname, "../../server/internal/mcp/consent_assets"),
    emptyOutDir: true,
    sourcemap: false,
    rolldownOptions: {
      input: path.resolve(__dirname, "src/consent-tools/main.tsx"),
      output: {
        format: "iife",
        entryFileNames: "consent-tools.js",
        assetFileNames: "consent-tools[extname]",
      },
    },
  },
});

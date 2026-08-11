import path from "node:path";

import tailwindcss from "@tailwindcss/vite";
import { defineConfig } from "vite";

// Storybook deliberately does not reuse the app's vite config: that one reads
// GRAM_* env vars, builds cel.wasm and hand-rolls vendor chunks, none of which
// the component gallery needs.
export default defineConfig({
  plugins: [tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "../src"),
      "@gram/client": path.resolve(import.meta.dirname, "../src/sdk/src"),
    },
  },
});

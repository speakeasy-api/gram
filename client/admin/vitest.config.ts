import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  // A real origin, not "": empty is the not-configured case.
  define: {
    __GRAM_APP_URL__: JSON.stringify("https://app.gram.test"),
  },
  // Tailwind, so a test can import the generated stylesheet and assert on
  // resolved declarations rather than on class strings.
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    // Off by default, and with it off an imported stylesheet resolves to an
    // empty string instead of the generated CSS. Narrowed to the one sheet a
    // test reads, so no other file changes behaviour.
    css: { include: [/index\.css/] },
    environment: "happy-dom",
  },
});

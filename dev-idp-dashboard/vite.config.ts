import { tanstackStart } from "@tanstack/react-start/plugin/vite";
import tailwindcss from "@tailwindcss/vite";
import viteReact from "@vitejs/plugin-react";
import path from "node:path";
import { defineConfig } from "vite";

if (!process.env["GRAM_DEVIDP_EXTERNAL_URL"]) {
  throw new Error(
    "GRAM_DEVIDP_EXTERNAL_URL must be set. Run `mise run zero:devidp` or set it in mise.local.toml.",
  );
}

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
    },
  },
  plugins: [tailwindcss(), tanstackStart({ srcDirectory: "src" }), viteReact()],
});

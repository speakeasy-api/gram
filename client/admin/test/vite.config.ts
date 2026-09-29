import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Separate build: never ship the browser fixture with the admin application.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: { alias: { "@": path.resolve(import.meta.dirname, "../src") } },
  build: {
    outDir: "node_modules/.registry-editor-test",
    rollupOptions: {
      input: path.resolve(import.meta.dirname, "registry-editor.html"),
    },
  },
  preview: { host: "127.0.0.1", port: 4179, strictPort: true },
});

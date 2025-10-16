import path from "node:path";
import fs from "node:fs";
import process from "node:process";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

let key: Buffer | undefined;
let cert: Buffer | undefined;

if (process.env["GRAM_SSL_KEY_FILE"] && process.env["GRAM_SSL_CERT_FILE"]) {
  key = fs.readFileSync(process.env["GRAM_SSL_KEY_FILE"]);
  cert = fs.readFileSync(process.env["GRAM_SSL_CERT_FILE"]);
}

// https://vite.dev/config/
export default defineConfig({
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(process.env["GRAM_SERVER_URL"]),
    __GRAM_GIT_SHA__: JSON.stringify(process.env["GRAM_GIT_SHA"] || ""),
  },
  build: {
    target: "es2022",
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: {
          "lucide-react": ["lucide-react"],
          moonshine: ["@speakeasy-api/moonshine"],
          externals: [
            "posthog-js",
            "react",
            "react-dom",
            "react-error-boundary",
            "react-router",
            "sonner",
            "vaul",
            "zod",
          ],
        },
      },
    },
  },
  esbuild: {
    target: "es2022",
  },
  optimizeDeps: {
    esbuildOptions: {
      target: "es2022",
    },
    exclude: ["@speakeasy-api/moonshine"],
  },
  server: {
    host: true,
    allowedHosts: ["localhost", "127.0.0.1", "devbox"],
    https: key && cert ? { key, cert } : void 0,
  },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
});

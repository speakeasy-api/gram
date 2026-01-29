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

const serverUrl = process.env["GRAM_SERVER_URL"];
if (!serverUrl) {
  throw new Error("GRAM_SERVER_URL environment variable is not set");
}

const siteUrl = process.env["GRAM_SITE_URL"];
if (!siteUrl) {
  throw new Error("GRAM_SITE_URL environment variable is not set");
}

// https://vite.dev/config/
export default defineConfig({
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(siteUrl),
    __GRAM_GIT_SHA__: JSON.stringify(process.env["GRAM_GIT_SHA"] || ""),
  },
  build: {
    target: "es2022",
    sourcemap: true,
    rollupOptions: {
      input: {
        main: path.resolve(__dirname, "index.html"),
      },
      output: {
        manualChunks: {
          "lucide-react": ["lucide-react"],
          moonshine: ["@speakeasy-api/moonshine"],
          three: [
            "@react-three/drei",
            "@react-three/fiber",
            "@react-three/postprocessing",
            "three",
          ],
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
    include: ["monaco-editor"],
    esbuildOptions: {
      target: "es2022",
    },
  },
  server: {
    host: true,
    allowedHosts: ["localhost", "127.0.0.1", "devbox"],
    https: key && cert ? { key, cert } : void 0,
    // Setting these up to side-step cors issues experienced during
    // development. Specifically, the Vercel AI SDK does not forward cookies
    // (Eg: gram_session) to the server.
    proxy: {
      "/rpc": serverUrl,
      "/chat": serverUrl,
      "/mcp": serverUrl,
      "/oauth": serverUrl,
      "/.well-known": serverUrl,
    },
  },
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
      // Ensure single instances of React and related packages across all dependencies
      react: path.resolve(__dirname, "node_modules/react"),
      "react-dom": path.resolve(__dirname, "node_modules/react-dom"),
      // Deduplicate @assistant-ui packages to ensure context is shared
      "@assistant-ui/react": path.resolve(
        __dirname,
        "node_modules/@assistant-ui/react",
      ),
      "@assistant-ui/react-markdown": path.resolve(
        __dirname,
        "node_modules/@assistant-ui/react-markdown",
      ),
    },
  },
});

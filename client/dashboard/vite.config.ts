import path from "node:path";
import fs from "node:fs";
import process from "node:process";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
// https://vite.dev/config/
export default defineConfig(({ command }) => {
  const isDev = command === "serve";

  // Dev HTTPS key/cert. Env vars are set repo-wide by mise.toml, but the
  // referenced files only exist on dev laptops — CI runners (and tools
  // like knip that load this config) get an ENOENT. Swallow the read
  // error so non-dev consumers of the config still work.
  let key: Buffer | undefined;
  let cert: Buffer | undefined;
  if (
    isDev &&
    process.env["GRAM_SSL_KEY_FILE"] &&
    process.env["GRAM_SSL_CERT_FILE"]
  ) {
    try {
      key = fs.readFileSync(process.env["GRAM_SSL_KEY_FILE"]);
      cert = fs.readFileSync(process.env["GRAM_SSL_CERT_FILE"]);
    } catch {
      // SSL files missing — fall through without HTTPS.
    }
  }

  const siteUrl = process.env["GRAM_SITE_URL"];
  if (isDev && !siteUrl) {
    throw new Error("GRAM_SITE_URL must be set in development");
  }

  const serverUrl = process.env["GRAM_SERVER_URL"];
  if (isDev && !serverUrl) {
    throw new Error("GRAM_SERVER_URL must be set in development");
  }

  // Two build-time constants, separated so MCP configs / callback URLs /
  // anything operator-facing always report the server's authoritative URL,
  // and only the playground (which needs same-origin cookie forwarding for
  // the Vercel AI SDK) routes through the dashboard origin via the vite
  // proxy.
  //
  //   __GRAM_SERVER_URL__       — the server's URL, always. Used everywhere
  //                               except the playground.
  //   __PLAYGROUND_PROXY_URL__  — the dashboard origin in dev (so the vite
  //                               proxy can ferry cookies); undefined in
  //                               prod (no proxy needed). Used only by the
  //                               playground.

  return {
    define: {
      __GRAM_SERVER_URL__: JSON.stringify(serverUrl),
      __PLAYGROUND_PROXY_URL__: JSON.stringify(isDev ? siteUrl : undefined),
      __GRAM_GIT_SHA__: JSON.stringify(process.env["GRAM_GIT_SHA"] || ""),
    },
    build: {
      target: "es2022",
      sourcemap: true,
      // Vite 8 defaults CSS minification to Lightning CSS, whose 1.32.0
      // build (pulled transitively by @tailwindcss/vite) panics on our CSS
      // (src/values/color.rs:441 SIGABRT). Pin CSS minification to esbuild —
      // the Vite 7 default — until the upstream Lightning CSS bug is fixed.
      cssMinify: "esbuild",
      rollupOptions: {
        input: {
          main: path.resolve(__dirname, "index.html"),
        },
        output: {
          // NOTE: trailing slashes (e.g. "node_modules/react/") are load-bearing
          // — they stop `react` from also matching react-dom/react-router/etc.
          // Don't drop them.
          manualChunks(id) {
            if (id.includes("node_modules/lucide-react")) return "lucide-react";
            if (id.includes("node_modules/@speakeasy-api/moonshine")) {
              return "moonshine";
            }
            // Keep the whole three.js ecosystem together (three plus the
            // packages reached only through @react-three/*: three-mesh-bvh,
            // three-stdlib, troika-*) so it stays out of the main chunk.
            if (
              id.includes("node_modules/@react-three/") ||
              id.includes("node_modules/three/") ||
              id.includes("node_modules/three-") ||
              id.includes("node_modules/troika-")
            ) {
              return "three";
            }
            if (
              id.includes("node_modules/posthog-js") ||
              id.includes("node_modules/react/") ||
              id.includes("node_modules/react-dom/") ||
              id.includes("node_modules/react-error-boundary") ||
              id.includes("node_modules/react-router") ||
              id.includes("node_modules/sonner") ||
              id.includes("node_modules/zod")
            ) {
              return "externals";
            }
          },
        },
      },
    },
    worker: {
      format: "es",
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
      proxy: serverUrl
        ? {
            "/rpc": serverUrl,
            "/chat": serverUrl,
            "/mcp": serverUrl,
            "/oauth": serverUrl,
            "/oauth-external": serverUrl,
            "/.well-known": serverUrl,
            "/v1": serverUrl,
          }
        : undefined,
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
  };
});

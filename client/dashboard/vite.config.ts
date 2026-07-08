import path from "node:path";
import fs from "node:fs";
import process from "node:process";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// Manually grouped vendor chunks. CAUTION: never group a package whose dist
// contains a top-level `await import(...)` (check before adding). Grouping
// pulls the package's shared internals into the group chunk, so the awaited
// sub-module ends up statically importing the very chunk that is suspended
// awaiting it — a silent module-evaluation deadlock that blank-screens the
// app. This took prod down for @speakeasy-api/moonshine (its dist top-level
// awaits ./speakeasy-logo-*.mjs), which is why it is absent from this list;
// Rolldown's automatic chunking handles it without creating the cycle.
const manualChunkGroups: [string, string[]][] = [
  ["lucide-react", ["lucide-react"]],
  [
    "three",
    [
      "@react-three/drei",
      "@react-three/fiber",
      "@react-three/postprocessing",
      "three",
    ],
  ],
  [
    "externals",
    [
      "posthog-js",
      "react",
      "react-dom",
      "react-error-boundary",
      "react-router",
      "sonner",
      "zod",
    ],
  ],
];

function packagePathRegex(packages: string[]): RegExp {
  const alternatives = packages.map((pkg) =>
    pkg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replaceAll("/", "[\\\\/]"),
  );
  return new RegExp(`node_modules[\\\\/](?:${alternatives.join("|")})[\\\\/]`);
}
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
  const devProxyServerUrl = process.env["GRAM_SERVER_BACKEND_URL"] || serverUrl;

  const allowedHosts = new Set(["localhost", "127.0.0.1", "devbox"]);
  for (const hostname of (process.env["VITE_DEV_HOSTNAMES"] || "").split(",")) {
    const trimmed = hostname.trim();
    if (trimmed) allowedHosts.add(trimmed);
  }

  const devProxyTarget = devProxyServerUrl
    ? {
        target: devProxyServerUrl,
        changeOrigin: true,
        secure: false,
      }
    : undefined;

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
      sourcemap: true,
      rolldownOptions: {
        input: {
          main: path.resolve(__dirname, "index.html"),
        },
        output: {
          codeSplitting: {
            groups: manualChunkGroups.map(([name, packages]) => ({
              name,
              test: packagePathRegex(packages),
            })),
          },
        },
      },
    },
    worker: {
      format: "es",
      // The worker bundles are pure vendor code (monaco's ts.worker map alone
      // is ~16MB, ~28MB across all workers) that we never debug in production,
      // so skip their sourcemaps while keeping maps for app code. This must be
      // an outputOptions hook: Vite hard-sets `sourcemap` to build.sourcemap
      // AFTER spreading worker.rolldownOptions.output, so the plain option is
      // silently ignored, while plugin outputOptions hooks run last.
      plugins: () => [
        {
          name: "drop-worker-sourcemaps",
          outputOptions(options) {
            return { ...options, sourcemap: false };
          },
        },
      ],
    },
    optimizeDeps: {
      include: ["monaco-editor"],
    },
    server: {
      host: true,
      allowedHosts: [...allowedHosts],
      https: key && cert ? { key, cert } : void 0,
      // Setting these up to side-step cors issues experienced during
      // development. Specifically, the Vercel AI SDK does not forward cookies
      // (Eg: gram_session) to the server.
      proxy: devProxyTarget
        ? {
            "/rpc": devProxyTarget,
            "/chat": devProxyTarget,
            "/mcp": devProxyTarget,
            "/oauth": devProxyTarget,
            "/oauth-external": devProxyTarget,
            "/.well-known": devProxyTarget,
            "/v1": devProxyTarget,
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

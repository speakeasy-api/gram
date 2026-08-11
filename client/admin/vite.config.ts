import fs from "node:fs";
import path from "node:path";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// In production the admin dashboard and the Gram admin API share one origin,
// so this config needs no CDN base and no build-time server URL.
//
// The dev proxy below reproduces that single origin locally. It is not the
// cross-origin machinery this app deliberately avoids: it is what lets the app
// keep relative paths, no CORS and no `credentials: 'include'` in dev too.
// 5173 belongs to the dashboard. Every parallel worktree gets its own admin
// API port from zero:remap-ports, but this port is not remapped, so a second
// worktree needs the override to run its own dev server.
const DEFAULT_DEV_PORT = 5174;

export default defineConfig(({ command }) => {
  const isDev = command === "serve";

  const devPort = Number(
    process.env["GRAM_ADMIN_DASHBOARD_PORT"] || DEFAULT_DEV_PORT,
  );
  if (isDev && !Number.isInteger(devPort)) {
    throw new Error("GRAM_ADMIN_DASHBOARD_PORT must be an integer");
  }

  // Dev HTTPS key/cert. mise.toml sets these vars repo-wide, but the files
  // only exist on dev machines. CI runners and config loaders such as knip get
  // an ENOENT, so swallow the read error and fall through to plain HTTP.
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
      // SSL files missing. Fall through without HTTPS.
    }
  }

  // Every admin API path, and the login redirect, live under /admin.
  const adminServerUrl = process.env["GRAM_ADMIN_SERVER_URL"];
  if (isDev && !adminServerUrl) {
    throw new Error("GRAM_ADMIN_SERVER_URL must be set in development");
  }

  return {
    plugins: [react(), tailwindcss()],
    build: {
      sourcemap: true,
    },
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    server: {
      // Fail on a taken port rather than sliding to the next one: the port is
      // part of the origin, and GRAM_ADMIN_ALLOWED_ORIGINS has to name it
      // exactly.
      port: devPort,
      strictPort: true,
      https: key && cert ? { key, cert } : undefined,
      proxy: adminServerUrl
        ? {
            "/admin": {
              target: adminServerUrl,
              changeOrigin: true,
              // The local admin API uses a self-signed certificate.
              secure: false,
            },
          }
        : undefined,
    },
  };
});

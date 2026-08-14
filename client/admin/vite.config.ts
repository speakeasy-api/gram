import fs from "node:fs";
import path from "node:path";

import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { tanstackRouter } from "@tanstack/router-plugin/vite";

// In production the admin dashboard and the Gram admin API share one origin,
// so this config needs no CDN base and no build-time server URL.
//
// The dev proxy below reproduces that single origin locally: this dev server
// is GRAM_ADMIN_SERVER_URL, and it forwards /admin to the admin API. It is not
// the cross-origin machinery this app deliberately avoids: it is what lets the
// app keep relative paths, no CORS and no `credentials: 'include'` in dev too.
// 5173 belongs to the Gram dashboard. mise.toml owns this port and
// zero:remap-ports randomizes it per worktree, so the fallback only matters
// when vite runs outside mise.
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

  // Every admin API path, and the login redirect, live under /admin. The
  // target is the API's own origin, never GRAM_ADMIN_SERVER_URL: that names
  // this dev server, so proxying to it would loop.
  const adminBackendUrl = process.env["GRAM_ADMIN_BACKEND_URL"];
  if (isDev && !adminBackendUrl) {
    throw new Error("GRAM_ADMIN_BACKEND_URL must be set in development");
  }

  // Baked in: a different origin, so there is no runtime way to learn it.
  // Empty disables the link.
  const appUrl = process.env["GRAM_APP_URL"] || "";
  // An operator authenticates at this origin, so plaintext is a downgrade.
  if (appUrl) {
    let parsed: URL;
    try {
      parsed = new URL(appUrl);
    } catch {
      throw new Error(`GRAM_APP_URL must be an absolute URL, got "${appUrl}"`);
    }
    if (parsed.protocol !== "https:") {
      throw new Error(
        `GRAM_APP_URL must use https, got "${parsed.protocol}//"`,
      );
    }
    if (!parsed.hostname) {
      throw new Error(`GRAM_APP_URL must name a host, got "${appUrl}"`);
    }
  }

  // Mirrors the Gram dashboard's config. Note this is NOT an access control:
  // vite skips its host-validation middleware entirely when the dev server is
  // HTTPS, which this one always is locally, and it exempts IP-literal Host
  // headers in any case. What the list still does is keep the HMR websocket
  // working when the app is loaded on a hostname other than localhost.
  const allowedHosts = new Set(["localhost", "127.0.0.1", "devbox"]);
  for (const hostname of (process.env["VITE_DEV_HOSTNAMES"] || "").split(",")) {
    const trimmed = hostname.trim();
    if (trimmed) allowedHosts.add(trimmed);
  }

  // Loopback by default. Binding every interface publishes the admin app —
  // whose local IdP accepts any identity without a password — to the whole
  // network, so it happens only once someone has opted into remote access via
  // zero:remap-hostname. docs/remote-dev-access.md spells out the exposure.
  const devHost = process.env["GRAM_DEV_HOSTNAME"] ? true : "localhost";

  return {
    define: {
      __GRAM_APP_URL__: JSON.stringify(appUrl),
    },
    plugins: [
      // The generator reads src/routes and writes src/routeTree.gen.ts. It has
      // to run before the react plugin, because the react plugin transforms the
      // route files it emits. autoCodeSplitting gives each route its own chunk.
      tanstackRouter({ target: "react", autoCodeSplitting: true }),
      react(),
      tailwindcss(),
    ],
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
      host: devHost,
      allowedHosts: [...allowedHosts],
      https: key && cert ? { key, cert } : undefined,
      proxy: adminBackendUrl
        ? {
            "/admin": {
              target: adminBackendUrl,
              changeOrigin: true,
              // The local admin API uses a self-signed certificate.
              secure: false,
            },
          }
        : undefined,
    },
  };
});

import path from "node:path";
import fs from "node:fs";
import process from "node:process";
import { pathToFileURL } from "node:url";
import { execFileSync } from "node:child_process";

import {
  defineConfig,
  mergeConfig,
  normalizePath,
  type ConfigEnv,
  type Plugin,
  type UserConfig,
} from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { replaceAdminServerUrl } from "./src/lib/admin-server-url.ts";

// Manually grouped vendor chunks. CAUTION: never group a package whose dist
// contains a top-level `await import(...)` (check before adding). Grouping
// pulls the package's shared internals into the group chunk, so the awaited
// sub-module ends up statically importing the very chunk that is suspended
// awaiting it — a silent module-evaluation deadlock that blank-screens the
// app. This once took prod down via a dependency whose dist top-level awaited
// a sibling module.
const manualChunkGroups: [string, string[]][] = [
  ["lucide-react", ["lucide-react"]],
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

const themeInitPath = path.resolve(import.meta.dirname, "src/theme-init.ts");
const themeInitScriptPattern =
  /<script src="\/src\/theme-init\.ts"\s*><\/script>/;

function themeInitPlugin(): Plugin {
  return {
    name: "theme-init",
    apply: "build",
    buildStart() {
      this.emitFile({
        type: "chunk",
        id: themeInitPath,
        name: "theme-init",
      });
    },
    transformIndexHtml: {
      order: "post",
      handler(html, context) {
        if (!context.bundle) {
          this.error("Theme bootstrap output bundle is unavailable");
        }

        const normalizedThemeInitPath = normalizePath(themeInitPath);
        const themeInitChunk = Object.values(context.bundle).find(
          (output) =>
            output.type === "chunk" &&
            output.facadeModuleId !== null &&
            normalizePath(output.facadeModuleId) === normalizedThemeInitPath,
        );
        if (!themeInitChunk) {
          this.error("Could not find the emitted theme bootstrap chunk");
        }
        if (!themeInitScriptPattern.test(html)) {
          this.error("Could not find the theme bootstrap script in index.html");
        }

        return html.replace(
          themeInitScriptPattern,
          `<script src="/${themeInitChunk.fileName}"></script>`,
        );
      },
    },
  };
}

// Feeds the development readout in the sidebar's brand row
// (src/components/dev-worktree-readout.tsx): which worktree this dev server is
// serving and what it has checked out. Baked in as constants that are empty in
// production builds, where the readout is compiled out.
const DEV_BRANCH_EVENT = "gram:dev-branch";
const DEV_BRANCH_ASK = "gram:dev-branch:ask";

function git(args: string[]): string {
  try {
    return execFileSync("git", args, {
      cwd: import.meta.dirname,
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    }).trim();
  } catch {
    return "";
  }
}

function currentBranch(): string {
  // A detached HEAD has no branch name — name the commit instead.
  return (
    git(["branch", "--show-current"]) || git(["rev-parse", "--short", "HEAD"])
  );
}

function currentWorktree(): string {
  const root = git(["rev-parse", "--show-toplevel"]);
  return root ? path.basename(root) : "";
}

// Keeps the readout honest across `git checkout` instead of going stale until
// the next dev-server restart. Vite's own watcher ignores **/.git/**, so watch
// the git directory with fs.watch. Watching the directory rather than HEAD
// itself survives git's write-a-lockfile-then-rename update, which replaces the
// inode a file watch is holding.
function devReadoutPlugin(): Plugin {
  return {
    name: "gram-dev-readout",
    apply: "serve",
    configureServer(server) {
      // The constant a client starts from was baked when this server started,
      // so a checkout since then has already made it stale for every page
      // loaded afterwards. Answer the asking client, not the whole room.
      server.hot.on(DEV_BRANCH_ASK, (_data, client) => {
        client.send(DEV_BRANCH_EVENT, currentBranch());
      });

      const gitDir = git(["rev-parse", "--absolute-git-dir"]);
      if (!gitDir) return;

      let timer: ReturnType<typeof setTimeout> | undefined;
      const watcher = fs.watch(gitDir, (_event, filename) => {
        if (filename !== "HEAD") return;
        clearTimeout(timer);
        timer = setTimeout(() => {
          server.hot.send(DEV_BRANCH_EVENT, currentBranch());
        }, 50);
      });
      watcher.on("error", () => watcher.close());
      server.httpServer?.once("close", () => {
        clearTimeout(timer);
        watcher.close();
      });
    },
  };
}

// Optional per-developer overrides, the same escape hatch mise.local.toml is:
// drop a gitignored vite.config.local.ts beside this file to load your own
// plugins or tweak settings without touching the config everyone shares.
// Default-export either a UserConfig or a function of the config env; it is
// merged last, so it wins. Errors in it are deliberately not swallowed — a
// broken local config should say so rather than silently do nothing.
const LOCAL_CONFIG_FILE = "vite.config.local.ts";

async function loadLocalConfig(env: ConfigEnv): Promise<UserConfig> {
  const file = path.resolve(import.meta.dirname, LOCAL_CONFIG_FILE);
  if (!fs.existsSync(file)) return {};

  // Built at runtime so the config bundler can't statically analyze it: the
  // file is optional and usually absent, and a literal specifier would make
  // that a hard dependency. Node strips the TS types on import.
  //
  // The mtime query busts Node's ESM cache, which is keyed by URL and lives as
  // long as the process: a restart re-imports this file in place, so without
  // the query an edit would restart the server and then quietly re-run the
  // previous version.
  const specifier = `${pathToFileURL(file).href}?mtime=${fs.statSync(file).mtimeMs}`;
  const loaded: unknown = await import(specifier);
  const local = (loaded as { default?: unknown }).default;
  const config =
    typeof local === "function"
      ? ((await local(env)) as UserConfig)
      : ((local ?? {}) as UserConfig);

  // Vite only watches config files it bundled, and this one is imported
  // outside that graph — restart on edits so it behaves like the real config.
  return mergeConfig(config, {
    plugins: [
      {
        name: "gram-local-config-watch",
        apply: "serve",
        configureServer(server) {
          server.watcher.add(file);
          server.watcher.on("change", (changed) => {
            if (changed === file) void server.restart();
          });
        },
      } satisfies Plugin,
    ],
  });
}

function packagePathRegex(packages: string[]): RegExp {
  const alternatives = packages.map((pkg) =>
    pkg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&").replaceAll("/", "[\\\\/]"),
  );
  return new RegExp(`node_modules[\\\\/](?:${alternatives.join("|")})[\\\\/]`);
}
// https://vite.dev/config/
export default defineConfig(async (env) => {
  const { command } = env;
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

  const config: UserConfig = {
    experimental: {
      // Static assets can be served through a CDN.
      // The CDN hostname may be env-specific but this build is env-agnostic
      // (one image is promoted dev -> prod), so instead of baking a CDN origin
      // in via `base`, the dashboard's nginx rewrites the /assets/ URLs in
      // index.html to the CDN host at serve time (see nginx.conf) and every
      // other URL follows from the module that references it:
      //
      //   - html: keep the default root-absolute /assets/... URLs so the
      //     nginx sub_filter can match and rewrite them.
      //   - js/css: emit URLs relative to import.meta.url so chunk imports,
      //     lazy-load preloads, and fonts/images referenced from CSS resolve
      //     against whatever origin the module graph was loaded from — the
      //     CDN when enabled, same-origin otherwise.
      //   - workers: keep the default root-absolute URLs. Browsers reject
      //     cross-origin new Worker(), so worker scripts must load from the
      //     app origin even when everything else is on the CDN (CSP's
      //     worker-src 'self' also depends on this). Detected by filename:
      //     every worker entry (monaco's *.worker, @pierre/diffs' worker.js)
      //     has "worker" in its emitted basename — keep it that way when
      //     adding new workers.
      renderBuiltUrl(filename, { hostType, type }) {
        const isWorkerAsset = /(^|\/)[^/]*worker[^/]*\.js$/.test(filename);
        if (
          (type === "asset" || type === "public") &&
          !isWorkerAsset &&
          (hostType === "js" || hostType === "css")
        ) {
          return { relative: true };
        }
        return undefined;
      },
    },
    define: {
      __GRAM_SERVER_URL__: JSON.stringify(serverUrl),
      __PLAYGROUND_PROXY_URL__: JSON.stringify(isDev ? siteUrl : undefined),
      __GRAM_GIT_SHA__: JSON.stringify(process.env["GRAM_GIT_SHA"] || ""),
      // Default Gram API URL baked into the inlined elements code
      // (src/elements/lib/api.ts); config.api.url overrides it at runtime.
      __GRAM_API_URL__: JSON.stringify(process.env["GRAM_API_URL"] || ""),
      __GRAM_DEV_WORKTREE__: JSON.stringify(isDev ? currentWorktree() : ""),
      __GRAM_DEV_BRANCH__: JSON.stringify(isDev ? currentBranch() : ""),
      __GRAM_DEV_BRANCH_EVENT__: JSON.stringify(isDev ? DEV_BRANCH_EVENT : ""),
      __GRAM_DEV_BRANCH_ASK__: JSON.stringify(isDev ? DEV_BRANCH_ASK : ""),
    },
    build: {
      sourcemap: true,
      // Fonts must stay as standalone asset files — inlining them as base64
      // bloats the CSS bundle and defeats CDN caching of the font files.
      // Returning undefined keeps Vite's default inlining behavior for
      // everything else.
      assetsInlineLimit(filePath) {
        if (/\.(?:woff2?|ttf|otf|eot)$/i.test(filePath)) {
          return false;
        }
        return undefined;
      },
      rolldownOptions: {
        input: {
          main: path.resolve(import.meta.dirname, "index.html"),
        },
        output: {
          codeSplitting: {
            groups: [
              // Generated SDK source (aliased as @gram/client) goes into its
              // own chunk so app-code changes don't churn its hash.
              {
                name: "gram-sdk",
                test: /[\\/]src[\\/]sdk[\\/]/,
              },
              ...manualChunkGroups.map(([name, packages]) => ({
                name,
                test: packagePathRegex(packages),
              })),
            ],
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
      // (Eg: gram_session) to the server. Prefixes the Go server owns must
      // stay in lockstep with gram-infra helm ingress, with local-only extras
      // (/v1, /oauth-external). /shared/skills is the dashboard SPA and is
      // intentionally omitted; /shared/handoffs is raw markdown and is not.
      proxy: devProxyTarget
        ? {
            "/rpc": devProxyTarget,
            "/otel": devProxyTarget,
            "/chat": devProxyTarget,
            "/mcp": devProxyTarget,
            "/oauth": devProxyTarget,
            "/oauth-external": devProxyTarget,
            "/.well-known": devProxyTarget,
            "/platform-mcp": devProxyTarget,
            "/v1": devProxyTarget,
            "/shared/handoffs": devProxyTarget,
          }
        : undefined,
    },
    plugins: [
      {
        name: "admin-server-url",
        transformIndexHtml(html) {
          if (command !== "serve") return html;
          return replaceAdminServerUrl(
            html,
            process.env["GRAM_ADMIN_SERVER_URL"] || "",
          );
        },
      },
      themeInitPlugin(),
      devReadoutPlugin(),
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: {
        "@": path.resolve(import.meta.dirname, "./src"),
        "@gram/client": path.resolve(import.meta.dirname, "./src/sdk/src"),
        // Ensure single instances of React and related packages across all dependencies
        react: path.resolve(import.meta.dirname, "node_modules/react"),
        "react-dom": path.resolve(
          import.meta.dirname,
          "node_modules/react-dom",
        ),
        // Deduplicate @assistant-ui packages to ensure context is shared
        "@assistant-ui/react": path.resolve(
          import.meta.dirname,
          "node_modules/@assistant-ui/react",
        ),
        "@assistant-ui/react-markdown": path.resolve(
          import.meta.dirname,
          "node_modules/@assistant-ui/react-markdown",
        ),
      },
    },
  };

  return mergeConfig(config, await loadLocalConfig(env));
});

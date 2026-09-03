import path from "node:path";

import { defineConfig } from "vite";
import tailwindcss from "@tailwindcss/vite";

// Standalone stylesheet build for the server-rendered MCP consent page. The
// output is embedded in the Go server binary (go:embed), so it must land under
// deterministic names with no JS entry of its own.
//
// `base` makes Vite rewrite the @font-face urls to the absolute path the Go
// server mounts the font handler at; the page inlines the stylesheet, so a
// relative url would resolve against the consent URL instead.
export default defineConfig({
  plugins: [tailwindcss()],
  publicDir: false,
  base: "/mcp/consent-fonts/",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  build: {
    // A sibling of consent_assets/, never a subdirectory of it: the island
    // build (vite.consent.config.ts) empties that directory, so nesting these
    // assets inside it means `build:consent` silently deletes them and the Go
    // embed stops compiling.
    outDir: path.resolve(
      __dirname,
      "../../server/internal/mcp/consent_page_assets",
    ),
    emptyOutDir: true,
    sourcemap: false,
    // The fonts are the point of shipping this stylesheet; inlining them as
    // data: URIs would triple the size of a document that renders with
    // Cache-Control: no-store.
    assetsInlineLimit: 0,
    assetsDir: "",
    rolldownOptions: {
      input: path.resolve(__dirname, "src/consent-page/consent-page.css"),
      output: {
        // The stylesheet is embedded and served inline, so it carries no hash;
        // the fonts are served as immutable URLs and carry one.
        assetFileNames: (asset: { names?: string[]; name?: string }) => {
          const name = asset.names?.[0] ?? asset.name ?? "";
          return name.endsWith(".css")
            ? "consent-page.css"
            : "[name]-[hash][extname]";
        },
      },
    },
  },
});

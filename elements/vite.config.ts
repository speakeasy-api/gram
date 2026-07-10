import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { readFileSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { defineConfig } from "vite";
import dts from "vite-plugin-dts";
import { externalizeDeps } from "vite-plugin-externalize-deps";

const __dirname = dirname(fileURLToPath(import.meta.url));
const declarationOutputDir = resolve(__dirname, "dist");
const packageImports = (
  JSON.parse(readFileSync(resolve(__dirname, "package.json"), "utf8")) as {
    imports: Record<string, string>;
  }
).imports;

function resolvePackageImport(specifier: string): string {
  const exactTarget = packageImports[specifier];
  if (exactTarget) {
    return exactTarget;
  }

  for (const [pattern, target] of Object.entries(packageImports)) {
    const wildcardIndex = pattern.indexOf("*");
    if (wildcardIndex === -1) {
      continue;
    }

    const prefix = pattern.slice(0, wildcardIndex);
    const suffix = pattern.slice(wildcardIndex + 1);
    if (specifier.startsWith(prefix) && specifier.endsWith(suffix)) {
      const wildcard = specifier.slice(
        prefix.length,
        -suffix.length || undefined,
      );
      return target.replace("*", wildcard);
    }
  }

  throw new Error(`Unknown package import in declaration output: ${specifier}`);
}

function rewritePackageImports(filePath: string, content: string): string {
  return content.replace(
    /(['"])(#elements\/[^'"]+)\1/g,
    (_match, quote: string, specifier: string) => {
      const sourceTarget = resolvePackageImport(specifier);
      const declarationTarget = resolve(
        declarationOutputDir,
        sourceTarget.replace(/^\.\/src\//, "").replace(/\.(?:ts|tsx|css)$/, ""),
      );
      let relativeTarget = relative(
        dirname(filePath),
        declarationTarget,
      ).replaceAll("\\", "/");
      if (!relativeTarget.startsWith(".")) {
        relativeTarget = `./${relativeTarget}`;
      }

      return `${quote}${relativeTarget}${quote}`;
    },
  );
}

export default defineConfig({
  plugins: [
    react(),
    dts({
      beforeWriteFile(filePath, content) {
        return { content: rewritePackageImports(filePath, content) };
      },
    }),
    tailwindcss(),

    // Automatically keep peerDependencies as they are defined in the package.json in sync
    // with the rolldownOptions.external list
    externalizeDeps({
      deps: false,
      peerDeps: true,
      optionalDeps: false,
      devDeps: false,
      // react-original is a virtual alias resolved by reactCompat() at consumer build time.
      // react-markdown/remark-gfm pull `decode-named-character-reference`, whose browser
      // build runs `document.createElement` at module top-level — bundling that breaks SSR
      // consumers (e.g. the Next.js example prerendering /chat). Externalize so the consumer's
      // bundler resolves the SSR-safe (node) variant instead.
      include: ["react-original", "react-markdown", "remark-gfm"],
    }),
  ],
  build: {
    cssCodeSplit: true,
    sourcemap: true,
    lib: {
      entry: {
        elements: resolve(__dirname, "src/index.ts"),
        "elements.css": resolve(__dirname, "src/global.css"),
        server: resolve(__dirname, "src/server.ts"),
        "server/core": resolve(__dirname, "src/server/core.ts"),
        "server/express": resolve(__dirname, "src/server/express.ts"),
        "server/nextjs": resolve(__dirname, "src/server/nextjs.ts"),
        "server/fastify": resolve(__dirname, "src/server/fastify.ts"),
        "server/hono": resolve(__dirname, "src/server/hono.ts"),
        "server/bun": resolve(__dirname, "src/server/bun.ts"),
        "server/tanstack-start": resolve(
          __dirname,
          "src/server/tanstack-start.ts",
        ),
        plugins: resolve(__dirname, "src/plugins/index.ts"),
        "compat-plugin": resolve(__dirname, "src/compat-plugin.ts"),
        "react-shim": resolve(__dirname, "src/react-shim.ts"),
      },
      formats: ["es"],
    },
    rolldownOptions: {
      // NOTE: do not define externals here, as they are defined in the externalizeDeps plugin
      output: {
        // react-shim.ts intentionally exports both a default (the whole shimmed
        // React object, aliased to `react`) and named APIs (useState, Fragment,
        // …), mirroring React's own dual interface. Rolldown can't auto-pick a
        // CJS export mode for a mixed entry, so pin "named": named APIs land on
        // module.exports directly, the default stays reachable via `.default`
        // with the standard __esModule interop marker.
        exports: "named",
        globals: {
          react: "React",
          "react-dom": "ReactDOM",
        },

        sourcemapExcludeSources: true,
      },
    },
  },
  define: {
    __GRAM_API_URL__: JSON.stringify(process.env["GRAM_API_URL"] || ""),
    __GRAM_GIT_SHA__: JSON.stringify(process.env["GRAM_GIT_SHA"] || ""),
  },
});

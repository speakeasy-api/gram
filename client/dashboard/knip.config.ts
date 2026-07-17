import type { KnipConfig } from "knip";

const config: KnipConfig = {
  // Vite entry (index.html → src/main.tsx) is auto-detected.
  // Emitted programmatically by themeInitPlugin, which Knip cannot infer.
  entry: ["src/theme-init.ts"],
  // Vitest, ESLint, Tailwind, and TypeScript plugins are auto-enabled.
  ignoreBinaries: [
    // Invoked from the lint:format script; not on the dep tree.
    "oxfmt",
    // Invoked from the prebuild script to build cel.wasm; not on the dep tree.
    "mise",
  ],
  ignore: [
    // Global ambient declarations (FIXME<M> escape-hatch + JSX namespace
    // re-export). No import sites by design.
    "src/lib.d.ts",
    "src/sdk/**/*",
    // Inlined Gram Elements library (formerly @gram-ai/elements). Its public
    // surface is wider than what the dashboard consumes today.
    "src/elements/**/*",
  ],
  ignoreDependencies: [
    // Consumed via CSS @import inside @speakeasy-api/moonshine; knip's
    // CSS plugin only scans first-party files so it misses this.
    "@tailwindcss/typography",
  ],
};

export default config;

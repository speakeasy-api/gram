import type { KnipConfig } from "knip";

const config: KnipConfig = {
  // Vite entry (index.html → src/main.tsx) is auto-detected.
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
  ],
  ignoreDependencies: [
    // Consumed via CSS @import inside @speakeasy-api/moonshine; knip's
    // CSS plugin only scans first-party files so it misses this.
    "@tailwindcss/typography",
  ],
};

export default config;

import type { KnipConfig } from "knip";

const config: KnipConfig = {
  // Vite entry (index.html → src/main.tsx) is auto-detected.
  // theme-init.ts is emitted programmatically by themeInitPlugin, and the two
  // consent entries live in vite.consent.config.ts and
  // vite.consent-page.config.ts, non-default config filenames — Knip cannot
  // infer any of them. The consent page's entry is the stylesheet itself: that
  // build emits CSS for the server-rendered page and has no JS.
  entry: [
    "src/theme-init.ts",
    "src/consent-tools/main.tsx",
    "src/consent-page/consent-page.css",
  ],
  // Vitest, ESLint, Tailwind, and TypeScript plugins are auto-enabled.
  ignoreBinaries: [
    // The package manager itself, used to chain scripts and to reach the
    // workspace-root oxfmt binary; not on the dep tree.
    "aube",
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
    // Internalised design system. Same reasoning: a component library exposes
    // its full API (Badge.Text, DropdownMenuSub, …) whether or not the app
    // happens to use every part of it today.
    "src/components/ui/**/*",
    // Page-template layer + its composite widgets: a shared page-shape library
    // (all templates + widgets) whose full API is exposed whether or not every
    // page has migrated onto it yet — same rationale as components/ui above.
    "src/components/page-templates/**/*",
  ],
};

export default config;

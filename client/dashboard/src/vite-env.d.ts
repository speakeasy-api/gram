/// <reference types="vite/client" />

declare const __GRAM_SERVER_URL__: string | undefined;
declare const __PLAYGROUND_PROXY_URL__: string | undefined;
declare const __GRAM_GIT_SHA__: string | undefined;
declare const __GRAM_API_URL__: string | undefined;

// Dev-only: which worktree/branch this dev server is serving, for the sidebar
// readout. Empty strings in production builds. See vite.config.ts.
declare const __GRAM_DEV_WORKTREE__: string;
declare const __GRAM_DEV_BRANCH__: string;
declare const __GRAM_DEV_BRANCH_EVENT__: string;
declare const __GRAM_DEV_BRANCH_ASK__: string;

interface ViteTypeOptions {
  // By adding this line, you can make the type of ImportMetaEnv strict
  // to disallow unknown keys.
  strictImportMetaEnv: unknown;
}

interface ImportMetaEnv {
  readonly VITE_DEV_HOSTNAMES?: string | undefined;
  readonly VITE_DATADOG_APPLICATION_ID?: string | undefined;
  readonly VITE_DATADOG_CLIENT_TOKEN?: string | undefined;
  readonly VITE_DATADOG_SITE?: string | undefined;
  readonly VITE_DATADOG_ENV?: string | undefined;
  readonly VITE_ELEMENTS_ENABLE_CASSETTE_RECORDING?: string | undefined;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

declare module "*.css?inline" {
  const content: string;
  export default content;
}

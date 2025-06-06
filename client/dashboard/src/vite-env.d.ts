/// <reference types="vite/client" />

declare const __GRAM_SERVER_URL__: string;

interface ViteTypeOptions {
  // By adding this line, you can make the type of ImportMetaEnv strict
  // to disallow unknown keys.
  strictImportMetaEnv: unknown;
}

interface ImportMetaEnv {
  readonly VITE_DEV_HOSTNAMES?: string | undefined;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}

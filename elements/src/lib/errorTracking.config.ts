/**
 * Datadog RUM configuration for Gram Elements.
 * Values are injected at build time via environment variables.
 * These client tokens are designed to be client-side safe.
 *
 * Required env vars for build:
 * - VITE_DATADOG_APPLICATION_ID
 * - VITE_DATADOG_CLIENT_TOKEN
 * - VITE_DATADOG_SITE (optional, defaults to datadoghq.com)
 * - VITE_DATADOG_ENV (optional, defaults to prod)
 */
import type { ElementsImportMetaEnv } from "#elements/types/env";

export const DATADOG_CONFIG = {
  applicationId:
    (import.meta.env as ElementsImportMetaEnv).VITE_DATADOG_APPLICATION_ID ??
    "",
  clientToken:
    (import.meta.env as ElementsImportMetaEnv).VITE_DATADOG_CLIENT_TOKEN ?? "",
  site:
    (import.meta.env as ElementsImportMetaEnv).VITE_DATADOG_SITE ??
    "datadoghq.com",
  env: (import.meta.env as ElementsImportMetaEnv).VITE_DATADOG_ENV ?? "prod",
  service: "gram-elements",
} as const;

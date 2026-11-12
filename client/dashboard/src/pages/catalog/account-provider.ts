import type { CatalogServer } from "./hooks";
import {
  isSnowflakeCatalogServer,
  snowflakeSetupUrl,
} from "./snowflake-install";
import { oktaRemoteTemplate, oktaSpecifier } from "./provider-setup/okta";

export type AccountProvider = "snowflake" | "okta";
export const accountSpecifiers = {
  snowflake: "com.snowflake/cortex-mcp",
  okta: oktaSpecifier,
};
export function accountCatalogProvider(
  server: CatalogServer,
): AccountProvider | null {
  if (isSnowflakeCatalogServer(server)) return "snowflake";
  if (
    server.registrySpecifier === oktaSpecifier &&
    server.remotes?.some(
      (r) =>
        r.url === oktaRemoteTemplate && r.transportType === "streamable-http",
    )
  )
    return "okta";
  return null;
}

// Disabled-only source markers. Setup replaces these before OAuth or testing.
export const accountSetupUrls: Record<AccountProvider, string> = {
  snowflake: snowflakeSetupUrl,
  okta: "https://www.okta.com/speakeasy-mcp-setup",
};
export function pendingAccountProvider(
  url: string | undefined,
): AccountProvider | null {
  for (const provider of ["snowflake", "okta"] as const) {
    if (url === accountSetupUrls[provider]) return provider;
  }
  return null;
}

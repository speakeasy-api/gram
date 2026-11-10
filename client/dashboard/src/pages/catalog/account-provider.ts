import type { CatalogServer } from "./hooks";
import { isSnowflakeCatalogServer } from "./snowflake-install";
import {
  snowflakeConnection,
  snowflakeSelectionForUrl,
} from "./provider-setup/snowflake";
import {
  oktaConnection,
  oktaSelectionForUrl,
  oktaRemoteTemplate,
  oktaSpecifier,
} from "./provider-setup/okta";
import type { State } from "./provider-setup/state";

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
export function accountSelection(
  provider: AccountProvider,
  url: string,
): Partial<State["values"]> | null {
  return provider === "okta"
    ? oktaSelectionForUrl(url)
    : snowflakeSelectionForUrl(url);
}
export function accountConnection(
  s: State,
): ReturnType<typeof oktaConnection> | ReturnType<typeof snowflakeConnection> {
  return s.provider === "okta"
    ? oktaConnection(s.values)
    : snowflakeConnection(s.values);
}

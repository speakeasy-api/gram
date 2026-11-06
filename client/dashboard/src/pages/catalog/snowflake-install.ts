import type { CatalogServer } from "./hooks";

export const snowflakeRemoteTemplate =
  "https://{account_url}/api/v2/databases/{mcp_database}/schemas/{mcp_schema}/mcp-servers/{mcp_server_name}";

/** Only the native account-specific catalog entry uses this setup handoff. */
export function isSnowflakeCatalogServer(server: CatalogServer): boolean {
  return (
    server.registrySpecifier === "com.snowflake/cortex-mcp" &&
    server.remotes?.some(
      (remote) =>
        remote.transportType === "streamable-http" &&
        remote.url === snowflakeRemoteTemplate,
    ) === true
  );
}

import type { CatalogServer } from "./hooks";

export const snowflakeRemoteTemplate =
  "https://{account_url}/api/v2/databases/{mcp_database}/schemas/{mcp_schema}/mcp-servers/{mcp_server_name}";

// Disabled-only source marker. Setup replaces it before configuring OAuth.
export const snowflakeSetupUrl =
  "https://app.snowflake.com/speakeasy-mcp-setup";

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

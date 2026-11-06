import { snowflakeConnection } from "./provider-setup/snowflake";

export const snowflakeRemoteTemplate =
  "https://{account_url}/api/v2/databases/{mcp_database}/schemas/{mcp_schema}/mcp-servers/{mcp_server_name}";
export type SnowflakeInstallValues = Partial<
  Record<
    "account_url" | "mcp_database" | "mcp_schema" | "mcp_server_name",
    string
  >
>;

export function isSnowflakeTemplate(specifier: string, url: string): boolean {
  return (
    specifier === "com.snowflake/cortex-mcp" && url === snowflakeRemoteTemplate
  );
}

/** Resolve only the reviewed native Snowflake template, using setup's validation. */
export function resolveSnowflakeInstall(
  values: SnowflakeInstallValues = {},
): string | null {
  const host = values.account_url?.trim() ?? "";
  if (!/^[a-z0-9._-]+$/i.test(host)) return null;
  return (
    snowflakeConnection({
      host,
      database: values.mcp_database ?? "",
      schema: values.mcp_schema ?? "",
      serverName: values.mcp_server_name ?? "",
    })?.url ?? null
  );
}

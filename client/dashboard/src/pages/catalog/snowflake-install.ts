import {
  snowflakeConnection,
  publicSnowflakeHost,
  idValid,
} from "./provider-setup/snowflake";

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

export function snowflakeInstallErrors(
  values: SnowflakeInstallValues = {},
): Partial<Record<keyof SnowflakeInstallValues, string>> {
  const errors: Partial<Record<keyof SnowflakeInstallValues, string>> = {};
  const host = values.account_url?.trim() ?? "";
  if (!host) errors.account_url = "Enter the public account hostname.";
  else if (!/^[a-z0-9._-]+$/i.test(host) || !publicSnowflakeHost(host))
    errors.account_url =
      "Use a public Snowflake hostname without https://, a path, port, or PrivateLink.";
  for (const field of [
    "mcp_database",
    "mcp_schema",
    "mcp_server_name",
  ] as const) {
    if (!idValid(values[field] ?? ""))
      errors[field] =
        "Enter a valid name. Use double quotes for spaces or case-sensitive names.";
  }
  return errors;
}

/** Resolve only the reviewed native Snowflake template, using setup's validation. */
export function resolveSnowflakeInstall(
  values: SnowflakeInstallValues = {},
): string | null {
  if (Object.keys(snowflakeInstallErrors(values)).length) return null;
  return (
    snowflakeConnection({
      host: values.account_url ?? "",
      database: values.mcp_database ?? "",
      schema: values.mcp_schema ?? "",
      serverName: values.mcp_server_name ?? "",
    })?.url ?? null
  );
}

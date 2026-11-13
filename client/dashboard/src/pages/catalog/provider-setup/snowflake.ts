export type SnowflakeValues = {
  host: string;
  network: string;
  method: string;
  database: string;
  schema: string;
  serverName: string;
  agent: string;
  toolName: string;
  toolTitle: string;
  toolDescription: string;
  creatorRole: string;
  accessRole: string;
  warehouse: string;
  users: string;
  oauthIntegration: string;
};

export function idValid(value: string): boolean {
  const name = value.trim();
  return (
    name.length <= 255 &&
    !Array.from(name).some(
      (char) => char.charCodeAt(0) < 32 || char.charCodeAt(0) === 127,
    ) &&
    (/^[A-Za-z_][A-Za-z0-9_$]*$/.test(name) || /^"(?:[^"]|"")+"$/.test(name))
  );
}
const identifier = (value: string): string =>
  value.trim().startsWith('"') ? value.trim() : value.trim().toUpperCase();
const rawIdentifier = (value: string): string =>
  identifier(value).replace(/^"|"$/g, "").replace(/""/g, '"');
export const creatorRoleAllowed = (value: string): boolean =>
  idValid(value) && rawIdentifier(value).toUpperCase() !== "PUBLIC";
export const mcpNameValid = (value: string): boolean =>
  idValid(value) && ![".", ".."].includes(rawIdentifier(value));
const identifierParts = (value: string): string[] =>
  value.trim().match(/"(?:[^"]|"")+"|[^.]+/g) ?? [];
export const agentValid = (value: string): boolean => {
  const parts = identifierParts(value);
  return (
    parts.length === 3 &&
    parts.every(idValid) &&
    parts.join(".") === value.trim()
  );
};
export const roleAllowed = (value: string): boolean =>
  idValid(value) &&
  ![
    "ACCOUNTADMIN",
    "SECURITYADMIN",
    "SYSADMIN",
    "USERADMIN",
    "GLOBALORGADMIN",
    "ORGADMIN",
    "PUBLIC",
  ].includes(rawIdentifier(value).toUpperCase());

export function users(values: Pick<SnowflakeValues, "users">): string[] {
  const result: string[] = [];
  let part = "",
    quoted = false;
  for (let index = 0; index < values.users.length; index++) {
    const char = values.users[index]!;
    if (char === '"') {
      if (quoted && values.users[index + 1] === '"') {
        part += '""';
        index++;
        continue;
      }
      quoted = !quoted;
    }
    if (!quoted && /[,\r\n]/.test(char)) {
      if (part.trim()) result.push(part.trim());
      part = "";
    } else part += char;
  }
  if (part.trim()) result.push(part.trim());
  return result;
}

/** Only public account endpoints; PrivateLink is a browser authorization option. */
export function publicSnowflakeHost(value: string): string | null {
  try {
    const input = value.trim();
    if (/:\d+(?:\/|$)/.test(input)) return null;
    const url = new URL(input.includes("://") ? input : `https://${input}`);
    url.hostname = url.hostname.replaceAll("_", "-");
    if (
      url.protocol !== "https:" ||
      url.port ||
      url.username ||
      url.password ||
      url.search ||
      url.hash ||
      url.pathname !== "/" ||
      !/^[a-z0-9-]+(?:\.[a-z0-9-]+)*\.snowflakecomputing\.com$/.test(
        url.hostname,
      ) ||
      /(?:^|\.)(?:privatelink|us-gov[^.]*|us-dod[^.]*)(?:\.|$)/.test(
        url.hostname,
      )
    )
      return null;
    return url.hostname;
  } catch {
    return null;
  }
}

type SnowflakeConnection = {
  name: string;
  issuer: string;
  url: string;
  authorizationEndpoint: string;
  tokenEndpoint: string;
  scopes: string[];
  authMethod: "client_secret_post";
};
export function snowflakeConnection(
  values: Pick<SnowflakeValues, "host" | "database" | "schema" | "serverName">,
): SnowflakeConnection | null {
  const host = publicSnowflakeHost(values.host);
  if (
    !host ||
    ![values.database, values.schema, values.serverName].every(mcpNameValid)
  )
    return null;
  const issuer = `https://${host}`;
  const part = (value: string) => encodeURIComponent(rawIdentifier(value));
  return {
    name: "Snowflake",
    issuer,
    url: `${issuer}/api/v2/databases/${part(values.database)}/schemas/${part(values.schema)}/mcp-servers/${part(values.serverName)}`,
    authorizationEndpoint: `${issuer}/oauth/authorize`,
    tokenEndpoint: `${issuer}/oauth/token-request`,
    scopes: ["session:role:all"],
    authMethod: "client_secret_post" as const,
  };
}

export function snowflakeSelectionForUrl(
  value: string | undefined,
): Partial<SnowflakeValues> | null {
  if (!value) return null;
  try {
    const url = new URL(value);
    if (
      !publicSnowflakeHost(url.origin) ||
      url.username ||
      url.password ||
      url.search ||
      url.hash
    )
      return null;
    const match = url.pathname
      .replace(/\/+$/, "")
      .match(
        /^\/api\/v2\/databases\/([^/]+)\/schemas\/([^/]+)\/mcp-servers\/([^/]+)$/,
      );
    if (!match) return null;
    const name = (part: string) => {
      const raw = decodeURIComponent(part);
      return /^[A-Z_][A-Z0-9_$]*$/.test(raw)
        ? raw
        : '"' + raw.replace(/"/g, '""') + '"';
    };
    const selection = {
      host: url.origin,
      database: name(match[1]!),
      schema: name(match[2]!),
      serverName: name(match[3]!),
    };
    return snowflakeConnection(selection) ? selection : null;
  } catch {
    return null;
  }
}

export const serverValid = (values: SnowflakeValues): boolean =>
  [values.database, values.schema, values.serverName].every(mcpNameValid) &&
  agentValid(values.agent);

/** Only complete statements are returned. These are displayed, never executed. */
export function snowflakeSql(
  v: SnowflakeValues,
  callback?: string,
): Partial<
  Record<
    | "creator"
    | "create"
    | "role"
    | "cortex"
    | "grants"
    | "users"
    | "oauth"
    | "credentials",
    string
  >
> {
  const result: ReturnType<typeof snowflakeSql> = {};
  const id = identifier,
    literal = (value: string) =>
      "'" + value.replaceAll("\\", "\\\\").replaceAll("'", "''") + "'";
  const a = identifierParts(v.agent).map(id),
    role = id(v.accessRole),
    creator = id(v.creatorRole);
  const ns = [v.database, v.schema].map(id).join("."),
    server = [v.database, v.schema, v.serverName].map(id).join(".");
  if (v.method === "new" && serverValid(v)) {
    if (creatorRoleAllowed(v.creatorRole))
      result.creator = `GRANT USAGE ON DATABASE ${id(v.database)} TO ROLE ${creator};\nGRANT USAGE ON SCHEMA ${ns} TO ROLE ${creator};\nGRANT CREATE MCP SERVER ON SCHEMA ${ns} TO ROLE ${creator};\nGRANT USAGE ON DATABASE ${a[0]} TO ROLE ${creator};\nGRANT USAGE ON SCHEMA ${a.slice(0, 2).join(".")} TO ROLE ${creator};\nGRANT USAGE ON AGENT ${a.join(".")} TO ROLE ${creator};`;
    if (
      /^[A-Za-z_][A-Za-z0-9_]*$/.test(v.toolName) &&
      v.toolTitle.trim() &&
      v.toolDescription.trim() &&
      ![v.agent, v.toolName, v.toolTitle, v.toolDescription].some((value) =>
        value.includes("$$"),
      )
    )
      result.create = `CREATE MCP SERVER ${server}\nFROM SPECIFICATION $$\ntools:\n  - title: ${JSON.stringify(v.toolTitle)}\n    name: ${JSON.stringify(v.toolName)}\n    type: CORTEX_AGENT_RUN\n    identifier: ${JSON.stringify(a.join("."))}\n    description: ${JSON.stringify(v.toolDescription)}\n$$;`;
  }
  if (roleAllowed(v.accessRole)) {
    const connecting = users(v);
    if (
      serverValid(v) &&
      idValid(v.warehouse) &&
      connecting.length &&
      connecting.every(idValid)
    ) {
      result.role = `CREATE ROLE IF NOT EXISTS ${role};`;
      result.cortex = `GRANT DATABASE ROLE SNOWFLAKE.CORTEX_AGENT_USER TO ROLE ${role};`;
      result.grants = `GRANT USAGE ON WAREHOUSE ${id(v.warehouse)} TO ROLE ${role};\nGRANT USAGE ON DATABASE ${id(v.database)} TO ROLE ${role};\nGRANT USAGE ON SCHEMA ${ns} TO ROLE ${role};\nGRANT USAGE ON MCP SERVER ${server} TO ROLE ${role};\nGRANT USAGE ON DATABASE ${a[0]} TO ROLE ${role};\nGRANT USAGE ON SCHEMA ${a.slice(0, 2).join(".")} TO ROLE ${role};\nGRANT USAGE ON AGENT ${a.join(".")} TO ROLE ${role};`;
      result.users = connecting
        .map(
          (user) =>
            `GRANT ROLE ${role} TO USER ${id(user)};\nALTER USER ${id(user)} SET DEFAULT_ROLE = ${literal(rawIdentifier(v.accessRole))}\n  DEFAULT_WAREHOUSE = ${literal(rawIdentifier(v.warehouse))};`,
        )
        .join("\n\n");
    }
    if (idValid(v.oauthIntegration) && callback)
      result.oauth = `CREATE SECURITY INTEGRATION ${id(v.oauthIntegration)}\n  TYPE = OAUTH\n  OAUTH_CLIENT = CUSTOM\n  ENABLED = TRUE\n  OAUTH_CLIENT_TYPE = 'CONFIDENTIAL'\n  OAUTH_REDIRECT_URI = ${literal(callback)}\n  OAUTH_USE_SECONDARY_ROLES = NONE\n${v.network === "private" ? "  USE_PRIVATELINK_FOR_AUTHORIZATION_ENDPOINT = TRUE\n" : ""}  ALLOWED_ROLES_LIST = (${literal(rawIdentifier(v.accessRole))});`;
  }
  if (idValid(v.oauthIntegration))
    result.credentials = `WITH oauth_secrets AS (\n  SELECT PARSE_JSON(\n    SYSTEM$SHOW_OAUTH_CLIENT_SECRETS(${literal(rawIdentifier(v.oauthIntegration))})\n  ) AS secret_values\n)\nSELECT\n  secret_values:"OAUTH_CLIENT_ID"::STRING AS "OAUTH_CLIENT_ID",\n  secret_values:"OAUTH_CLIENT_SECRET"::STRING AS "OAUTH_CLIENT_SECRET"\nFROM oauth_secrets;`;
  return result;
}

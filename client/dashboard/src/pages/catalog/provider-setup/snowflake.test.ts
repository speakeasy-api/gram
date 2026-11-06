import { describe, expect, it } from "vitest";
import {
  snowflakeConnection,
  snowflakeSelectionForUrl,
  snowflakeSql,
  publicSnowflakeHost,
} from "./snowflake";

const values = {
  host: "https://example-org-example-account.snowflakecomputing.com/",
  network: "public",
  method: "new",
  database: "example_db",
  schema: "mcp",
  serverName: "example_server",
  agent: "AGENT_DB.AGENTS.EXAMPLE_AGENT",
  toolName: "example_agent",
  toolTitle: "Example agent",
  toolDescription: "Answers questions.",
  creatorRole: "EXAMPLE_CREATOR",
  accessRole: "EXAMPLE_MCP_USER",
  warehouse: "EXAMPLE_WH",
  users: "EXAMPLE_USER",
  oauthIntegration: "example_oauth",
};

describe("Snowflake native setup contract", () => {
  it("configures the native account endpoint and confidential OAuth", () => {
    expect(snowflakeConnection(values)).toMatchObject({
      url: "https://example-org-example-account.snowflakecomputing.com/api/v2/databases/EXAMPLE_DB/schemas/MCP/mcp-servers/EXAMPLE_SERVER",
      issuer: "https://example-org-example-account.snowflakecomputing.com",
      authorizationEndpoint:
        "https://example-org-example-account.snowflakecomputing.com/oauth/authorize",
      tokenEndpoint:
        "https://example-org-example-account.snowflakecomputing.com/oauth/token-request",
      authMethod: "client_secret_post",
      scopes: ["session:role:all"],
    });
  });
  it.each([
    "http://example.snowflakecomputing.com",
    "example.privatelink.snowflakecomputing.com",
    "https://example.snowflakecomputing.com/path",
    "https://example.snowflakecomputing.com?token=x",
    "https://user@example.snowflakecomputing.com",
    "example.snowflakecomputing.com.evil.test",
    "https://example.snowflakecomputing.com:8443",
    "example.snowflakecomputing.cn",
  ])("rejects unsupported account addresses: %s", (host) => {
    expect(publicSnowflakeHost(host)).toBeNull();
  });
  it("restores exact case-sensitive server names from the installed endpoint", () => {
    const url =
      "https://example.snowflakecomputing.com/api/v2/databases/Mixed%20DB/schemas/MCP/mcp-servers/CaseSensitive";
    const selection = snowflakeSelectionForUrl(url);
    expect(selection).toMatchObject({
      database: '"Mixed DB"',
      serverName: '"CaseSensitive"',
    });
    expect(snowflakeConnection({ ...values, ...selection })?.url).toBe(url);
    expect(
      snowflakeSelectionForUrl(
        "https://app.getgram.ai/mcp/speakeasy-team-snowflake",
      ),
    ).toBeNull();
  });
  it("uses the actual callback and uppercase credential JSON keys", () => {
    const sql = snowflakeSql(
      values,
      "https://local.example/mcp/remote_login_callback",
    );
    expect(sql.oauth).toContain(
      "OAUTH_REDIRECT_URI = 'https://local.example/mcp/remote_login_callback'",
    );
    expect(sql.oauth).toContain("OAUTH_USE_SECONDARY_ROLES = NONE");
    expect(sql.credentials).toContain('secret_values:"OAUTH_CLIENT_ID"');
    expect(sql.credentials).toContain('secret_values:"OAUTH_CLIENT_SECRET"');
    expect(sql.credentials).toContain(
      "SYSTEM$SHOW_OAUTH_CLIENT_SECRETS('EXAMPLE_OAUTH')",
    );
  });
  it("withholds incomplete or unsafe executable SQL", () => {
    expect(snowflakeSql(values).oauth).toBeUndefined();
    expect(snowflakeSql({ ...values, database: "" }).create).toBeUndefined();
    expect(snowflakeSql({ ...values, users: "" }).grants).toBeUndefined();
    expect(
      snowflakeSql({ ...values, agent: 'DB.SCHEMA."agent$$; DROP ROLE x;--"' })
        .create,
    ).toBeUndefined();
    expect(
      snowflakeSql({ ...values, toolDescription: "$$; SELECT 1;" }).create,
    ).toBeUndefined();
    expect(
      snowflakeSql(
        { ...values, accessRole: "ACCOUNTADMIN" },
        "https://local.example/callback",
      ).oauth,
    ).toBeUndefined();
  });
  it("limits PrivateLink to browser authorization while keeping public upstream URLs", () => {
    const selected = { ...values, network: "private" };
    expect(
      snowflakeSql(selected, "https://local.example/callback").oauth,
    ).toContain("USE_PRIVATELINK_FOR_AUTHORIZATION_ENDPOINT = TRUE");
    expect(snowflakeConnection(selected)?.issuer).toBe(
      "https://example-org-example-account.snowflakecomputing.com",
    );
  });
  it("escapes literals and retains quoted identifiers in statements", () => {
    const sql = snowflakeSql(
      {
        ...values,
        accessRole: '"example\'s role"',
        users: '"user,one"\n"second.user"',
      },
      "https://local.example/callback",
    );
    expect(sql.oauth).toContain("ALLOWED_ROLES_LIST = ('example''s role')");
    expect(sql.users).toContain('TO USER "user,one"');
    expect(sql.users).toContain('TO USER "second.user"');
  });
});

it("rejects an explicit default port rather than silently normalizing it", () => {
  expect(
    publicSnowflakeHost("https://example.snowflakecomputing.com:443"),
  ).toBeNull();
});

it("normalizes the copied account hostname consistently for MCP and OAuth", () => {
  const connection = snowflakeConnection({
    ...values,
    host: "https://example-example_account.snowflakecomputing.com/",
  });
  expect(connection?.issuer).toBe(
    "https://example-example-account.snowflakecomputing.com",
  );
  expect(connection?.url).toContain(
    "https://example-example-account.snowflakecomputing.com/",
  );
  expect(connection?.authorizationEndpoint).toBe(
    "https://example-example-account.snowflakecomputing.com/oauth/authorize",
  );
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  isSnowflakeTemplate,
  resolveSnowflakeInstall,
  snowflakeRemoteTemplate,
} from "./snowflake-install";
const values = {
  account_url: "example.snowflakecomputing.com",
  mcp_database: "APP",
  mcp_schema: "PUBLIC",
  mcp_server_name: "MCP",
};
describe("Snowflake catalog handoff", () => {
  it("matches only the approved native record and exact template", () => {
    expect(
      isSnowflakeTemplate("com.snowflake/cortex-mcp", snowflakeRemoteTemplate),
    ).toBe(true);
    expect(isSnowflakeTemplate("other/provider", snowflakeRemoteTemplate)).toBe(
      false,
    );
    expect(
      isSnowflakeTemplate(
        "com.snowflake/cortex-mcp",
        "https://other.example/mcp",
      ),
    ).toBe(false);
  });
  it.each([
    "https://example.snowflakecomputing.com",
    "example.privatelink.snowflakecomputing.com",
    "example.snowflakecomputing.com:443",
    "example.snowflakecomputing.com/path",
  ])("rejects invalid account hostname %s", (account_url) => {
    expect(resolveSnowflakeInstall({ ...values, account_url })).toBeNull();
  });
  it.each(Object.keys(values))("requires %s", (key) => {
    expect(resolveSnowflakeInstall({ ...values, [key]: "" })).toBeNull();
  });
  it("encodes case-sensitive identifiers without changing the hostname", () => {
    expect(
      resolveSnowflakeInstall({ ...values, mcp_database: '"Mixed DB"' }),
    ).toBe(
      "https://example.snowflakecomputing.com/api/v2/databases/Mixed%20DB/schemas/PUBLIC/mcp-servers/MCP",
    );
  });
});

it("normalizes copied account names with underscores to the equivalent hostname", () => {
  expect(
    resolveSnowflakeInstall({
      ...values,
      account_url: "example-example_account.snowflakecomputing.com",
    }),
  ).toBe(
    "https://example-example-account.snowflakecomputing.com/api/v2/databases/APP/schemas/PUBLIC/mcp-servers/MCP",
  );
});

it.each(['"."', '".."'])(
  "blocks normalized path segments in catalog names: %s",
  (name) => {
    for (const field of [
      "mcp_database",
      "mcp_schema",
      "mcp_server_name",
    ] as const)
      expect(resolveSnowflakeInstall({ ...values, [field]: name })).toBeNull();
  },
);

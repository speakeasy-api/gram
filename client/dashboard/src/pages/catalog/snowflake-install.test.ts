// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  isSnowflakeCatalogServer,
  snowflakeRemoteTemplate,
} from "./snowflake-install";
const entry = {
  registrySpecifier: "com.snowflake/cortex-mcp",
  name: "Snowflake",
  description: "Snowflake",
  supportsDcr: false,
  version: "1.0.0",
  remotes: [
    { transportType: "streamable-http" as const, url: snowflakeRemoteTemplate },
  ],
};
describe("Snowflake catalog handoff", () => {
  it("matches only the native streamable HTTP template", () => {
    expect(isSnowflakeCatalogServer(entry)).toBe(true);
    expect(
      isSnowflakeCatalogServer({
        ...entry,
        registrySpecifier: "com.pulsemcp.mirror/gram-snowflake",
      }),
    ).toBe(false);
    expect(
      isSnowflakeCatalogServer({
        ...entry,
        remotes: [
          {
            transportType: "streamable-http",
            url: "https://other.example/mcp",
          },
        ],
      }),
    ).toBe(false);
    expect(
      isSnowflakeCatalogServer({
        ...entry,
        remotes: [{ transportType: "sse", url: snowflakeRemoteTemplate }],
      }),
    ).toBe(false);
  });
});

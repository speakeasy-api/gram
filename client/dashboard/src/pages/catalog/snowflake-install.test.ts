import { defaultServerGuardrailState } from "@/pages/security/server-guardrails/server-guardrail-policy";
import { describe, expect, it } from "vitest";
import {
  readSnowflakeHandoff,
  writeSnowflakeHandoff,
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

it("round-trips only the User Identity handoff and its existing modal choices", () => {
  sessionStorage.clear();
  expect(readSnowflakeHandoff("draft")).toBeNull();
  const guardrail = defaultServerGuardrailState();
  guardrail.categories = new Set(["pii", "secrets"]);
  guardrail.audienceType = "targeted";
  guardrail.audiencePrincipalUrns = new Set(["urn:example:principal"]);
  guardrail.action = "block";
  guardrail.score = 3;
  guardrail.userMessage = "Review the selected tools before retrying.";
  guardrail.toolMode = "selected";
  guardrail.selectedTools = ["example_tool"];
  expect(
    writeSnowflakeHandoff("draft", {
      name: "Example Snowflake",
      guardrail,
      customize: true,
    }),
  ).toBe(true);
  expect(readSnowflakeHandoff("draft")).toEqual({
    name: "Example Snowflake",
    guardrail,
    customize: true,
  });
  sessionStorage.setItem(
    "draft:handoff",
    JSON.stringify({ name: "Example Snowflake", identityMode: "none" }),
  );
  expect(readSnowflakeHandoff("draft")).toBeNull();
});

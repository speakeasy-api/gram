import { describe, expect, it } from "vitest";
import type { ToolUsageTraceSummary } from "@gram/client/models/components/toolusagetracesummary.js";
import { summarizeAgentCredentialUse } from "./agentCredentialUse";

function trace(
  overrides: Partial<ToolUsageTraceSummary>,
): ToolUsageTraceSummary {
  return {
    clientKey: "client",
    clientLabel: "Client",
    eventSource: "gateway",
    gramUrn: "tools:http:linear:list_issues",
    id: "trace",
    logCount: 1,
    logGroup: "server",
    startTimeUnixNano: "0",
    targetId: "linear",
    targetKind: "server",
    targetLabel: "Linear",
    targetType: "hosted_mcp_server",
    toolName: "list_issues",
    userKey: "agent-1",
    userKind: "agent_id",
    userLabel: "Release bot",
    ...overrides,
  } as ToolUsageTraceSummary;
}

describe("summarizeAgentCredentialUse", () => {
  it("names the person whose connection signed the agent's calls in", () => {
    const rows = summarizeAgentCredentialUse([
      trace({
        authClientType: "remote_session",
        connectionOwnerUserId: "user-ada",
        connectionOwnerLabel: "Ada Lovelace",
      }),
      trace({
        authClientType: "remote_session",
        connectionOwnerUserId: "user-ada",
        connectionOwnerLabel: "Ada Lovelace",
      }),
    ]);

    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      serverLabel: "Linear",
      credentialLabel: "Ada Lovelace's connection",
      personUserId: "user-ada",
      shared: false,
      calls: 2,
    });
  });

  it("marks a shared credential as acting for no one", () => {
    const [row] = summarizeAgentCredentialUse([
      trace({
        targetId: "github",
        targetLabel: "GitHub",
        authClientType: "service_account",
        authClientId: "client-1",
      }),
    ]);

    expect(row).toMatchObject({
      serverLabel: "GitHub",
      credentialLabel: "Shared credential",
      personUserId: undefined,
      shared: true,
    });
  });

  it("keeps one row per server and credential, busiest first", () => {
    const rows = summarizeAgentCredentialUse([
      trace({ authClientType: "service_account" }),
      trace({
        authClientType: "remote_session",
        connectionOwnerUserId: "user-ada",
        connectionOwnerLabel: "Ada Lovelace",
      }),
      trace({
        authClientType: "remote_session",
        connectionOwnerUserId: "user-ada",
        connectionOwnerLabel: "Ada Lovelace",
      }),
      trace({ targetId: "notion", targetLabel: "Notion" }),
    ]);

    expect(rows.map((row) => [row.serverLabel, row.credentialLabel])).toEqual([
      ["Linear", "Ada Lovelace's connection"],
      ["Linear", "Shared credential"],
      ["Notion", undefined],
    ]);
  });

  it("says the viewer's own connection is theirs", () => {
    const [row] = summarizeAgentCredentialUse(
      [
        trace({
          authClientType: "remote_session",
          connectionOwnerUserId: "user-ada",
          connectionOwnerLabel: "Ada Lovelace",
        }),
      ],
      "user-ada",
    );

    expect(row?.credentialLabel).toBe("Your connection (Ada Lovelace)");
  });
});

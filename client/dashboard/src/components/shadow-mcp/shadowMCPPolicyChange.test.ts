import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import {
  keepAsTodayDecision,
  serversChangedByPolicy,
  shadowMCPPolicySentence,
  standingDecisionOf,
} from "./shadowMCPPolicyChange";
import { testAccessSummary } from "./shadowMCPInventoryTestFixtures";

function server(
  name: string,
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    canonicalServerUrl: `https://${name}.example.com/mcp`,
    serverSlug: name,
    urlHost: `${name}.example.com`,
    serverName: name,
    firstSeen: new Date("2026-11-01T00:00:00Z"),
    lastSeen: new Date("2026-11-18T00:00:00Z"),
    observedUseCount: 0,
    blockedCallCount: 0,
    userCount: 0,
    topUsers: [],
    access: "allowed",
    accessSummary: testAccessSummary("allowed"),
    requestCount: 0,
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    ...overrides,
  };
}

function decided(
  name: string,
  decision: "approved" | "denied",
): ShadowMCPInventoryServer {
  const summary = testAccessSummary("allowed");
  return server(name, {
    accessSummary: summary ? { ...summary, decision } : undefined,
  });
}

describe("serversChangedByPolicy", () => {
  it("lists nothing when the policy does not flip", () => {
    expect(serversChangedByPolicy([server("a")], "allowed", "allowed")).toEqual(
      [],
    );
  });

  it("leaves out decided, linked and local servers", () => {
    const changed = serversChangedByPolicy(
      [
        server("undecided"),
        decided("approved", "approved"),
        decided("denied", "denied"),
        server("linked", {
          officialServer: {
            mcpServerId: "11111111-1111-4111-8111-111111111111",
            name: "Linked Official",
            directAccessAllowed: false,
          },
        }),
        server("link-off", {
          officialServer: {
            mcpServerId: "22222222-2222-4222-8222-222222222222",
            name: "Link Off Official",
            directAccessAllowed: true,
          },
        }),
        server("local", { targetKind: "stdio_command" }),
      ],
      "allowed",
      "blocked",
    );
    expect(changed.map((s) => s.serverName)).toEqual(["link-off", "undecided"]);
  });

  it("puts the most blocked, then the most used, servers first", () => {
    const changed = serversChangedByPolicy(
      [
        server("quiet"),
        server("busy", { observedUseCount: 40 }),
        server("blocked-a-lot", { observedUseCount: 5, blockedCallCount: 9 }),
        server("blocked-once", { observedUseCount: 90, blockedCallCount: 1 }),
      ],
      "blocked",
      "allowed",
    );
    expect(changed.map((s) => s.serverName)).toEqual([
      "blocked-a-lot",
      "blocked-once",
      "busy",
      "quiet",
    ]);
  });
});

describe("standingDecisionOf", () => {
  it("reads the access summary, then the review's standing decision", () => {
    expect(standingDecisionOf(decided("a", "denied"))).toBe("denied");
    expect(
      standingDecisionOf(
        server("b", {
          approvalRequest: {
            id: "request-1",
            status: "approved",
            requesterCount: 0,
            standingDecision: "approved",
          } as ShadowMCPInventoryServer["approvalRequest"],
        }),
      ),
    ).toBe("approved");
    expect(standingDecisionOf(server("c"))).toBeUndefined();
  });
});

describe("keepAsTodayDecision", () => {
  it("approves before blocking and blocks before allowing", () => {
    expect(keepAsTodayDecision("blocked")).toBe("approved");
    expect(keepAsTodayDecision("allowed")).toBe("denied");
  });
});

describe("shadowMCPPolicySentence", () => {
  it("states the policy in one line", () => {
    expect(shadowMCPPolicySentence("allowed", null)).toBe(
      "All Shadow MCPs are allowed by default.",
    );
    expect(shadowMCPPolicySentence("blocked", "")).toBe(
      "Block by default applies to nobody, so Shadow MCPs are allowed by default.",
    );
    expect(shadowMCPPolicySentence("blocked", null)).toBe(
      "All Shadow MCPs are blocked by default.",
    );
    expect(shadowMCPPolicySentence("blocked", "2 roles")).toBe(
      "Shadow MCPs are blocked by default for 2 roles. Everyone else is allowed by default.",
    );
  });
});

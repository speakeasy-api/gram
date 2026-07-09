import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import {
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusDescription,
  shadowMCPPolicyState,
} from "./shadowMCPInventoryStatus";

function policy(overrides: Partial<RiskPolicy>): RiskPolicy {
  return {
    action: "flag",
    audiencePrincipalUrns: ["user:all"],
    audienceType: "everyone",
    autoName: false,
    createdAt: new Date("2026-01-01T00:00:00Z"),
    enabled: true,
    id: "policy-1",
    name: "Policy",
    pendingMessages: 0,
    policyType: "standard",
    projectId: "project-1",
    sources: ["shadow_mcp"],
    totalMessages: 0,
    updatedAt: new Date("2026-01-01T00:00:00Z"),
    version: 1,
    ...overrides,
  };
}

function server(
  overrides: Partial<ShadowMCPInventoryServer>,
): ShadowMCPInventoryServer {
  return {
    access: "none",
    allowedPolicyIds: [],
    canonicalServerUrl: "https://example.com/mcp",
    firstSeen: new Date("2026-01-01T00:00:00Z"),
    lastSeen: new Date("2026-01-02T00:00:00Z"),
    observedUseCount: 0,
    requestCount: 0,
    serverName: undefined,
    topUsers: [],
    urlHost: "example.com",
    userCount: 0,
    ...overrides,
  };
}

describe("shadowMCPPolicyState", () => {
  it("prioritizes blocking policies over flagging policies", () => {
    expect(
      shadowMCPPolicyState([
        policy({ action: "flag", id: "flag" }),
        policy({ action: "block", id: "block" }),
      ]),
    ).toBe("blocking");
  });

  it("returns flagging for enabled flag policy without blocking policy", () => {
    expect(shadowMCPPolicyState([policy({ action: "flag" })])).toBe("flagging");
  });

  it("returns none when no enabled Shadow MCP policy exists", () => {
    expect(
      shadowMCPPolicyState([
        policy({ enabled: false }),
        policy({ sources: ["prompt_injection"] }),
      ]),
    ).toBe("none");
  });
});

describe("shadowMCPInventoryStatus", () => {
  it("shows allowed when a URL has an allow rule", () => {
    expect(
      shadowMCPInventoryStatus(server({ access: "allowed" }), "blocking"),
    ).toBe("allowed");
  });

  it("shows blocked when blocking is enabled and no allow rule exists", () => {
    expect(
      shadowMCPInventoryStatus(server({ access: "none" }), "blocking"),
    ).toBe("blocked");
  });

  it("shows observed when blocking is inactive", () => {
    expect(
      shadowMCPInventoryStatus(server({ access: "none" }), "flagging"),
    ).toBe("observed");
    expect(shadowMCPInventoryStatus(server({ access: "none" }), "none")).toBe(
      "observed",
    );
  });

  it("describes the status source", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "allowed" }),
        "blocking",
      ),
    ).toBe("Allowed by URL rule");
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "none" }),
        "blocking",
      ),
    ).toBe("Blocked by policy");
    expect(
      shadowMCPInventoryStatusDescription(server({ access: "none" }), "none"),
    ).toBe("Not blocking");
  });
});

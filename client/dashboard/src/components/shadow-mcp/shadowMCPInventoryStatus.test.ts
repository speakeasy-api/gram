import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import {
  eligibleShadowMCPAllowRulePolicies,
  shadowMCPBlockingPolicyDisposition,
  shadowMCPInventoryStatus,
  shadowMCPInventoryStatusBadgeVariant,
  shadowMCPInventoryStatusDescription,
  shadowMCPInventoryStatusLabel,
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
    score: 5,
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
    blockedPolicyIds: [],
    canonicalServerUrl: "https://example.com/mcp",
    firstSeen: new Date("2026-01-01T00:00:00Z"),
    lastSeen: new Date("2026-01-02T00:00:00Z"),
    observedUseCount: 0,
    requestCount: 0,
    serverName: undefined,
    serverSlug: "example-com-mcp-c3d80a4e",
    topUsers: [],
    urlHost: "example.com",
    userCount: 0,
    ...overrides,
  };
}

describe("eligibleShadowMCPAllowRulePolicies", () => {
  it("returns only enabled blocking Shadow MCP policies", () => {
    const policies = [
      policy({ action: "block", id: "eligible" }),
      policy({ action: "flag", id: "flag" }),
      policy({ action: "block", enabled: false, id: "disabled" }),
      policy({
        action: "block",
        id: "other-source",
        sources: ["prompt_injection"],
      }),
    ];

    expect(
      eligibleShadowMCPAllowRulePolicies(policies).map((item) => item.id),
    ).toEqual(["eligible"]);
  });

  it("returns an empty list while policies are unavailable", () => {
    expect(eligibleShadowMCPAllowRulePolicies(undefined)).toEqual([]);
  });
});

describe("shadowMCPPolicyState", () => {
  it("prioritizes blocking policies over warning and flagging policies", () => {
    expect(
      shadowMCPPolicyState([
        policy({ action: "flag", id: "flag" }),
        policy({ action: "warn", id: "warn" }),
        policy({ action: "block", id: "block" }),
      ]),
    ).toBe("blocking");
  });

  it("returns warning for enabled warn policy without blocking policy", () => {
    expect(shadowMCPPolicyState([policy({ action: "warn" })])).toBe("warning");
  });

  it("prioritizes warning policies over flagging policies", () => {
    expect(
      shadowMCPPolicyState([
        policy({ action: "flag", id: "flag" }),
        policy({ action: "warn", id: "warn" }),
      ]),
    ).toBe("warning");
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
  it("shows pending when a URL has access requests", () => {
    expect(
      shadowMCPInventoryStatus(
        server({ access: "blocked", requestCount: 2 }),
        "blocking",
      ),
    ).toBe("pending");
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "blocked", requestCount: 2 }),
        "blocking",
      ),
    ).toBe("2 access requests pending");
  });

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
      shadowMCPInventoryStatus(server({ access: "none" }), "warning"),
    ).toBe("observed");
    expect(
      shadowMCPInventoryStatus(server({ access: "none" }), "flagging"),
    ).toBe("observed");
    expect(shadowMCPInventoryStatus(server({ access: "none" }), "none")).toBe(
      "observed",
    );
  });

  it("shows unknown when policy state is unavailable", () => {
    expect(
      shadowMCPInventoryStatus(server({ access: "none" }), "unavailable"),
    ).toBe("unavailable");
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "none" }),
        "unavailable",
      ),
    ).toBe("Policy status unavailable");
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

  it("credits a denied review alongside the policy in the blocked source", () => {
    const denied = {
      id: "request-1",
      status: "denied" as const,
      requesterCount: 0,
      evidenceChangedAt: undefined,
    };
    // block_all: the policy already blocks and the deny compounds it.
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "blocked", approvalRequest: denied }),
        "blocking",
        "block_all",
      ),
    ).toBe("Blocked by policy & review");
    // The frontend-only blocking branch (access still "none") credits it too.
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "none", approvalRequest: denied }),
        "blocking",
      ),
    ).toBe("Blocked by policy & review");
    // allow_all: the block is the review's doing, so it stands alone.
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "blocked", approvalRequest: denied }),
        "blocking",
        "allow_all",
      ),
    ).toBe("Blocked by review");
  });

  it("surfaces a targeted block as restricted rather than blocked", () => {
    const restricted = server({ access: "restricted" });
    expect(shadowMCPInventoryStatus(restricted, "blocking")).toBe("restricted");
    expect(shadowMCPInventoryStatusLabel("restricted")).toBe("Restricted");
    expect(shadowMCPInventoryStatusBadgeVariant("restricted")).toBe("warning");
    expect(
      shadowMCPInventoryStatusDescription(restricted, "blocking", "block_all"),
    ).toBe("Blocked for some users");
    // A denied review outranks the targeted policy: the deny is definitive, so
    // the row is blocked rather than restricted-for-some.
    const deniedRestricted = server({
      access: "restricted",
      approvalRequest: {
        id: "request-1",
        status: "denied" as const,
        requesterCount: 0,
        evidenceChangedAt: undefined,
      },
    });
    expect(shadowMCPInventoryStatus(deniedRestricted, "blocking")).toBe(
      "blocked",
    );
    // Disposition-aware, like the blocked branch: under allow_all the deny is
    // the whole block; under block_all the policy blocks too.
    expect(
      shadowMCPInventoryStatusDescription(
        deniedRestricted,
        "blocking",
        "allow_all",
      ),
    ).toBe("Blocked by review");
    expect(
      shadowMCPInventoryStatusDescription(
        deniedRestricted,
        "blocking",
        "block_all",
      ),
    ).toBe("Blocked by policy & review");
  });

  it("leaves the blocked source unchanged when a review approved or is absent", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        server({
          access: "blocked",
          approvalRequest: {
            id: "request-2",
            status: "approved" as const,
            requesterCount: 0,
            evidenceChangedAt: undefined,
          },
        }),
        "blocking",
        "block_all",
      ),
    ).toBe("Blocked by policy");
    expect(
      shadowMCPInventoryStatusDescription(
        server({ access: "blocked" }),
        "blocking",
        "allow_all",
      ),
    ).toBe("Blocked by rule");
  });
});

describe("shadowMCPBlockingPolicyDisposition", () => {
  it("returns null with no blocking policies", () => {
    expect(shadowMCPBlockingPolicyDisposition([])).toBeNull();
  });

  it("returns allow_all only when every blocking policy declares it", () => {
    expect(
      shadowMCPBlockingPolicyDisposition([
        { shadowMcpDisposition: "allow_all" },
      ]),
    ).toBe("allow_all");
    expect(
      shadowMCPBlockingPolicyDisposition([
        { shadowMcpDisposition: "allow_all" },
        { shadowMcpDisposition: undefined },
      ]),
    ).toBe("block_all");
  });
});

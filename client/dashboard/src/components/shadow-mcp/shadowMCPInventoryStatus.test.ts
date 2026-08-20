import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import type { ShadowMCPAccessSummary } from "@gram/client/models/components/shadowmcpaccesssummary.js";
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

function summary(
  overrides: Partial<ShadowMCPAccessSummary>,
): ShadowMCPAccessSummary {
  return {
    state: "unenforced",
    allowedFor: "none",
    blockedFor: "none",
    blockingDefault: "none",
    decision: undefined,
    decisionCoverage: "none",
    ...overrides,
  };
}

function access(
  overrides: Partial<ShadowMCPAccessSummary>,
  requestCount = 0,
): Pick<ShadowMCPInventoryServer, "access" | "accessSummary" | "requestCount"> {
  return { access: "none", accessSummary: summary(overrides), requestCount };
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

  it("returns unavailable while policies have not loaded", () => {
    expect(shadowMCPPolicyState(undefined)).toBe("unavailable");
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

describe("shadowMCPInventoryStatus", () => {
  it("maps the summary state onto the badge status", () => {
    expect(shadowMCPInventoryStatus(access({ state: "allowed" }))).toBe(
      "allowed",
    );
    expect(shadowMCPInventoryStatus(access({ state: "blocked" }))).toBe(
      "blocked",
    );
    expect(shadowMCPInventoryStatus(access({ state: "restricted" }))).toBe(
      "restricted",
    );
    expect(shadowMCPInventoryStatus(access({ state: "unenforced" }))).toBe(
      "observed",
    );
  });

  it("lets open requests outrank the steady state", () => {
    expect(shadowMCPInventoryStatus(access({ state: "allowed" }, 2))).toBe(
      "pending",
    );
    expect(
      shadowMCPInventoryStatusDescription(access({ state: "allowed" }, 2)),
    ).toBe("2 access requests pending");
  });

  it("keeps a denied review restricted when only a targeted policy carries it", () => {
    // The mirror of the scoped-approval bug: a denial only a targeted policy
    // enforces is not a project-wide block, so the badge must not claim one.
    expect(
      shadowMCPInventoryStatus(
        access({
          state: "restricted",
          decision: "denied",
          decisionCoverage: "partial",
        }),
      ),
    ).toBe("restricted");
  });
});

describe("shadowMCPInventoryStatusLabel and badge", () => {
  it("labels each status", () => {
    expect(shadowMCPInventoryStatusLabel("restricted")).toBe("Restricted");
    expect(shadowMCPInventoryStatusLabel("observed")).toBe("Observed");
  });

  it("keeps orange for partial access and blue for pending", () => {
    expect(shadowMCPInventoryStatusBadgeVariant("restricted")).toBe("warning");
    expect(shadowMCPInventoryStatusBadgeVariant("pending")).toBe("information");
  });
});

describe("shadowMCPAccessSummaryOf fallback", () => {
  it("synthesizes a coarse summary from the legacy access field", () => {
    // The skew window: a rolled-back server sends no summary; the badge must
    // still be right even if the wording is generic.
    expect(
      shadowMCPInventoryStatus({
        access: "blocked",
        accessSummary: undefined,
        requestCount: 0,
      }),
    ).toBe("blocked");
    expect(
      shadowMCPInventoryStatusDescription({
        access: "restricted",
        accessSummary: undefined,
        requestCount: 0,
      }),
    ).toBe("Blocked for some users");
  });
});

describe("shadowMCPInventoryStatusDescription", () => {
  it("names the allow mechanism", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "allowed",
          allowedFor: "everyone",
          decision: "approved",
          decisionCoverage: "full",
          blockingDefault: "deny",
        }),
      ),
    ).toBe("Allowed by review");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "allowed",
          allowedFor: "everyone",
          blockingDefault: "deny",
        }),
      ),
    ).toBe("Allowed by URL rule");
    expect(
      shadowMCPInventoryStatusDescription(
        access({ state: "allowed", blockingDefault: "allow" }),
      ),
    ).toBe("Allowed by default");
  });

  it("names the block mechanism and credits an enforced denial", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({ state: "blocked", blockingDefault: "deny" }),
      ),
    ).toBe("Blocked by policy");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "blocked",
          blockedFor: "everyone",
          blockingDefault: "allow",
        }),
      ),
    ).toBe("Blocked by rule");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "blocked",
          blockingDefault: "deny",
          decision: "denied",
          decisionCoverage: "full",
        }),
      ),
    ).toBe("Blocked by policy & review");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "blocked",
          blockedFor: "everyone",
          blockingDefault: "allow",
          decision: "denied",
          decisionCoverage: "full",
        }),
      ),
    ).toBe("Blocked by review");
  });

  it("names each restricted flavor", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "restricted",
          blockedFor: "some",
          blockingDefault: "allow",
        }),
      ),
    ).toBe("Blocked for some users");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "restricted",
          allowedFor: "selected",
          blockingDefault: "deny",
        }),
      ),
    ).toBe("Allowed for selected users");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "restricted",
          allowedFor: "selected",
          blockedFor: "some",
          blockingDefault: "allow",
        }),
      ),
    ).toBe("Allowed for some, blocked for others");
  });

  it("reports a partially enforced denial as such", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "restricted",
          blockedFor: "some",
          blockingDefault: "allow",
          decision: "denied",
          decisionCoverage: "partial",
        }),
      ),
    ).toBe("Denied — enforced for the policy's audience only");
  });

  it("surfaces a decision the current rules contradict", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "allowed",
          allowedFor: "everyone",
          blockingDefault: "allow",
          decision: "denied",
          decisionCoverage: "partial",
        }),
      ),
    ).toBe("Allowed — a recorded denial is not enforced");
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "blocked",
          blockedFor: "everyone",
          blockingDefault: "allow",
          decision: "approved",
          decisionCoverage: "partial",
        }),
      ),
    ).toBe("Blocked — a recorded approval is not enforced");
    // Standing grants outliving a newer denial name the real mechanism.
    expect(
      shadowMCPInventoryStatusDescription(
        access({
          state: "restricted",
          allowedFor: "selected",
          blockingDefault: "deny",
          decision: "denied",
          decisionCoverage: "partial",
        }),
      ),
    ).toBe("Denied — standing grants still allow some users");
  });

  it("flags a dormant decision no policy enforces", () => {
    expect(
      shadowMCPInventoryStatusDescription(
        access({ state: "unenforced", decision: "denied" }),
      ),
    ).toBe("Denied — not enforced until a blocking policy exists");
    expect(
      shadowMCPInventoryStatusDescription(
        access({ state: "unenforced", decision: "approved" }),
      ),
    ).toBe("Approved — enforced once a blocking policy exists");
    expect(
      shadowMCPInventoryStatusDescription(access({ state: "unenforced" })),
    ).toBe("Not blocking");
  });
});

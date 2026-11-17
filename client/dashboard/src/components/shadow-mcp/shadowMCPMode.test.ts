import { describe, expect, it } from "vitest";
import type { RiskPolicy } from "@gram/client/models/components/riskpolicy.js";
import { shadowMCPModeOf } from "./shadowMCPMode";
import { isShadowMCPModePolicy } from "@/pages/security/policy-type";

function policy(overrides: Partial<RiskPolicy>): RiskPolicy {
  return {
    id: "policy-1",
    name: "Shadow MCP",
    enabled: true,
    sources: ["shadow_mcp"],
    action: "block",
    ...overrides,
  } as RiskPolicy;
}

describe("shadowMCPModeOf", () => {
  it("allows by default when there is no blocking policy", () => {
    expect(shadowMCPModeOf([])).toEqual({
      mode: "allowed",
      policy: undefined,
      heldByShadowAI: false,
    });
    expect(shadowMCPModeOf([policy({ action: "flag" })]).mode).toBe("allowed");
    expect(shadowMCPModeOf([policy({ enabled: false })]).mode).toBe("allowed");
  });

  it("blocks by default under an allowlist policy", () => {
    const blocking = policy({ shadowMcpDisposition: "block_all" });
    expect(shadowMCPModeOf([blocking])).toEqual({
      mode: "blocked",
      policy: blocking,
      heldByShadowAI: false,
    });
    // A policy from before dispositions existed blocks by default.
    expect(shadowMCPModeOf([policy({})]).mode).toBe("blocked");
  });

  it("allows by default under a Shadow AI policy, which holds the slot", () => {
    const shadowAI = policy({ shadowMcpDisposition: "allow_all" });
    expect(shadowMCPModeOf([shadowAI])).toEqual({
      mode: "allowed",
      policy: shadowAI,
      heldByShadowAI: true,
    });
  });
});

describe("isShadowMCPModePolicy", () => {
  it("hides the blocked-by-default policy from Guardrails, not Shadow AI", () => {
    expect(
      isShadowMCPModePolicy(policy({ shadowMcpDisposition: "block_all" })),
    ).toBe(true);
    expect(isShadowMCPModePolicy(policy({}))).toBe(true);
    expect(
      isShadowMCPModePolicy(policy({ shadowMcpDisposition: "allow_all" })),
    ).toBe(false);
    expect(isShadowMCPModePolicy(policy({ action: "flag" }))).toBe(false);
    expect(
      isShadowMCPModePolicy(policy({ sources: ["shadow_mcp", "gitleaks"] })),
    ).toBe(false);
  });
});

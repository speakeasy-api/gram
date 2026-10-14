import { describe, expect, it } from "vitest";
import {
  findBlockingShadowMCPPolicy,
  isShadowAIPolicy,
  policyTypeOf,
} from "./policy-type";

const shadowMCPBlock = {
  policyType: "standard" as const,
  sources: ["shadow_mcp"],
  action: "block" as const,
};

describe("policyTypeOf", () => {
  it("reads a prompt-based policy from its policy type", () => {
    expect(
      policyTypeOf({ ...shadowMCPBlock, policyType: "prompt_based" }),
    ).toBe("prompt");
  });

  it("reads a blocking shadow_mcp-only policy as Shadow AI", () => {
    expect(policyTypeOf(shadowMCPBlock)).toBe("shadow_ai");
    expect(isShadowAIPolicy(shadowMCPBlock)).toBe(true);
  });

  it("keeps a flagging shadow_mcp policy built-in", () => {
    expect(policyTypeOf({ ...shadowMCPBlock, action: "flag" })).toBe(
      "built_in",
    );
  });

  it("keeps a blocking policy with another detector built-in", () => {
    expect(
      policyTypeOf({ ...shadowMCPBlock, sources: ["shadow_mcp", "gitleaks"] }),
    ).toBe("built_in");
  });

  it("keeps a detector-only policy built-in", () => {
    expect(policyTypeOf({ ...shadowMCPBlock, sources: ["gitleaks"] })).toBe(
      "built_in",
    );
  });
});

describe("findBlockingShadowMCPPolicy", () => {
  it("returns the enabled blocking shadow_mcp policy, mixed sources included", () => {
    const mixed = {
      id: "mixed",
      enabled: true,
      sources: ["shadow_mcp", "gitleaks"],
      action: "block" as const,
    };
    expect(
      findBlockingShadowMCPPolicy([
        { id: "flag", enabled: true, sources: ["shadow_mcp"], action: "flag" },
        mixed,
      ]),
    ).toBe(mixed);
  });

  it("ignores a disabled blocking policy", () => {
    expect(
      findBlockingShadowMCPPolicy([
        { id: "off", enabled: false, sources: ["shadow_mcp"], action: "block" },
      ]),
    ).toBeUndefined();
  });
});

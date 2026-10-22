import type { RiskResult } from "@gram/client/models/components/riskresult.js";
import { unrecognized } from "@gram/client/types/unrecognized.js";
import { describe, expect, it } from "vitest";
import { isRestorable, suppressionReason } from "./suppressed-helpers";

const result: RiskResult = {
  id: "result-1",
  createdAt: new Date("2026-01-01"),
  policyId: "policy-1",
  policyVersion: 1,
  source: "prompt_injection",
};

describe("suppression restoration", () => {
  it("does not treat an unknown suppression reason as a manual dismissal", () => {
    const unknown = {
      ...result,
      suppressedReason: unrecognized("future_reason"),
    };
    expect(suppressionReason(unknown)).toBe("unknown");
    expect(isRestorable(unknown)).toBe(false);
  });

  it("still restores manual and automated dismissals but keeps rule suppressions", () => {
    expect(isRestorable({ ...result, suppressedReason: "manual" })).toBe(true);
    expect(isRestorable({ ...result, suppressedReason: "automated" })).toBe(
      true,
    );
    expect(isRestorable({ ...result, suppressedReason: "rule" })).toBe(false);
  });
});

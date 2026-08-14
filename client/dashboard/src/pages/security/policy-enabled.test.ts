import { describe, expect, it } from "vitest";
import {
  policyEnabledActionLabel,
  policyStatusLabel,
} from "./policy-enabled";

describe("policyEnabledActionLabel", () => {
  it("offers Disable while the policy is enforcing", () => {
    expect(policyEnabledActionLabel(true)).toBe("Disable");
  });

  it("offers Enable while the policy is inactive", () => {
    expect(policyEnabledActionLabel(false)).toBe("Enable");
  });
});

describe("policyStatusLabel", () => {
  it("labels an enabled policy as Enforcing", () => {
    expect(policyStatusLabel(true)).toBe("Enforcing");
  });

  it("labels a disabled policy as Inactive", () => {
    expect(policyStatusLabel(false)).toBe("Inactive");
  });
});

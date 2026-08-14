import { describe, expect, it } from "vitest";
import { togglePolicyEnabledVariables } from "./use-toggle-policy-enabled";

describe("togglePolicyEnabledVariables", () => {
  it("sends only the policy id and enabled flag", () => {
    expect(togglePolicyEnabledVariables("policy-1", false)).toEqual({
      request: {
        updateRiskPolicyRequestBody: { id: "policy-1", enabled: false },
      },
    });
    expect(togglePolicyEnabledVariables("policy-1", true)).toEqual({
      request: {
        updateRiskPolicyRequestBody: { id: "policy-1", enabled: true },
      },
    });
  });
});

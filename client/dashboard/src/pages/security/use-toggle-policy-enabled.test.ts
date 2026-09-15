import { describe, expect, it } from "vitest";
import { togglePolicyEnabledVariables } from "./use-toggle-policy-enabled";

describe("togglePolicyEnabledVariables", () => {
  it("sends the policy id, name, and enabled flag", () => {
    expect(
      togglePolicyEnabledVariables("policy-1", "Secrets Flagger", false),
    ).toEqual({
      request: {
        updateRiskPolicyRequestBody: {
          id: "policy-1",
          name: "Secrets Flagger",
          enabled: false,
        },
      },
    });
    expect(
      togglePolicyEnabledVariables("policy-1", "Secrets Flagger", true),
    ).toEqual({
      request: {
        updateRiskPolicyRequestBody: {
          id: "policy-1",
          name: "Secrets Flagger",
          enabled: true,
        },
      },
    });
  });
});

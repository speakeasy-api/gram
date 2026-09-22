import { describe, expect, it } from "vitest";
import {
  defaultPlannerState,
  recommendation,
  surfaceForHookSource,
  targetCapabilities,
} from "./support-matrix-model";

describe("support coverage planner model", () => {
  it("turns selected outcomes into a unique capability target", () => {
    expect(targetCapabilities(["gateway", "security", "cost"])).toEqual([
      "session",
      "blocking",
      "cost",
    ]);
  });

  it("does not recommend enterprise-only inference hooks for a team plan", () => {
    const result = recommendation({
      ...defaultPlannerState,
      answers: { ...defaultPlannerState.answers, plan: "Team" },
    });

    expect(result?.id).toBe("client-hooks");
  });

  it("does not recommend an integration that covers no target cells", () => {
    expect(
      recommendation({
        ...defaultPlannerState,
        outcomes: ["identity"],
        surfaces: ["cowork"],
        answers: { plan: "Team", mdm: "No MDM" },
      }),
    ).toBeNull();
  });

  it.each([
    ["claude", "chat"],
    ["claude-chat-web", "chat"],
    ["claude-code", "cc"],
    ["claude-code-web", "cc"],
    ["claude-code-desktop", "cc"],
    ["cowork-desktop", "cowork"],
    ["cursor", "cursor"],
    ["codex-cli", "codex"],
    ["gemini", "other"],
  ] as const)("maps %s telemetry to the %s surface", (source, surface) => {
    expect(surfaceForHookSource(source)).toBe(surface);
  });
});

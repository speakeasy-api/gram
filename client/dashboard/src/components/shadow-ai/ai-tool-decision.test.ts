import type { AIDetection } from "@gram/client/models/components/aidetection.js";
import { describe, expect, it } from "vitest";
import { aiToolDecisionOf, nextAIToolDecision } from "./ai-tool-decision";

function detection(
  access: Partial<NonNullable<AIDetection["access"]>> | undefined,
): AIDetection {
  return {
    targetId: "cursor",
    displayName: "Cursor",
    category: "harness",
    userCount: 1,
    deviceCount: 1,
    signals: ["installed"],
    versions: [],
    firstSeen: new Date("2026-09-01T00:00:00Z"),
    lastSeen: new Date("2026-09-10T00:00:00Z"),
    access: access
      ? {
          state: "unreviewed",
          decision: "unreviewed",
          enforceable: true,
          ...access,
        }
      : undefined,
  };
}

describe("aiToolDecisionOf", () => {
  it("reads approved and blocked decisions", () => {
    expect(aiToolDecisionOf(detection({ decision: "approved" }))).toBe(
      "approved",
    );
    expect(aiToolDecisionOf(detection({ decision: "blocked" }))).toBe(
      "blocked",
    );
  });

  it("reads an enforceable tool with no decision as unreviewed", () => {
    expect(aiToolDecisionOf(detection({ decision: "unreviewed" }))).toBe(
      "unreviewed",
    );
  });

  it("offers no decision for a tool the gateway cannot recognize", () => {
    expect(aiToolDecisionOf(detection({ enforceable: false }))).toBe(
      "inventory_only",
    );
    expect(aiToolDecisionOf(detection(undefined))).toBe("inventory_only");
  });
});

describe("nextAIToolDecision", () => {
  it("records the pressed option", () => {
    expect(nextAIToolDecision("unreviewed", "approved")).toBe("approved");
    expect(nextAIToolDecision("approved", "blocked")).toBe("blocked");
  });

  it("withdraws the option already in force", () => {
    expect(nextAIToolDecision("approved", "approved")).toBe("unreviewed");
    expect(nextAIToolDecision("blocked", "blocked")).toBe("unreviewed");
  });
});

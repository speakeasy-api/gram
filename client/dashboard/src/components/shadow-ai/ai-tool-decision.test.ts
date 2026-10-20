import { describe, expect, it } from "vitest";
import { nextAIToolDecision } from "./ai-tool-decision";

describe("nextAIToolDecision", () => {
  it("records the pressed option", () => {
    expect(nextAIToolDecision("unreviewed", "blocked")).toBe("blocked");
    expect(nextAIToolDecision("blocked", "approved")).toBe("approved");
  });

  it("withdraws the option already in force", () => {
    expect(nextAIToolDecision("blocked", "blocked")).toBe("unreviewed");
    expect(nextAIToolDecision("approved", "approved")).toBe("unreviewed");
  });
});

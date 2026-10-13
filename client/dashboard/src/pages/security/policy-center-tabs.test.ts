import { describe, expect, it } from "vitest";
import { policyCenterTabs, visiblePolicyCenterTab } from "./policy-center-tabs";

describe("policyCenterTabs", () => {
  it("adds the Shadow AI targets tab after Detection Rules once the fold-in is on", () => {
    const tabs = policyCenterTabs(true);
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "shadow-ai-targets",
      "exclusions",
      "quarantines",
    ]);
    expect(tabs.find((tab) => tab.value === "shadow-ai-targets")).toMatchObject(
      {
        label: "Shadow AI targets",
        href: "?tab=shadow-ai-targets",
      },
    );
  });

  it("keeps the four tabs while the fold-in is off", () => {
    expect(policyCenterTabs(false).map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "exclusions",
      "quarantines",
    ]);
  });
});

describe("visiblePolicyCenterTab", () => {
  it("falls back to Policies for a Shadow AI targets link opened with the fold-in off", () => {
    expect(visiblePolicyCenterTab("shadow-ai-targets", false)).toBe("policies");
    expect(visiblePolicyCenterTab("shadow-ai-targets", true)).toBe(
      "shadow-ai-targets",
    );
    expect(visiblePolicyCenterTab("exclusions", false)).toBe("exclusions");
  });
});

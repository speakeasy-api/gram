import { describe, expect, it } from "vitest";
import { policyCenterTabs } from "./policy-center-tabs";

describe("policyCenterTabs", () => {
  it("shows the Shadow AI targets tab after Detection Rules", () => {
    const tabs = policyCenterTabs();
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
});

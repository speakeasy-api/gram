// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  movedToShadowAITab,
  policyCenterTabs,
  policyCenterTabSearch,
  resolvePolicyCenterTab,
} from "./policy-center-tabs";

describe("policyCenterTabs", () => {
  it("keeps Shadow AI tabs out of Guardrails", () => {
    const tabs = policyCenterTabs();
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "exclusions",
      "quarantines",
    ]);
  });

  it("shows Tool approvals after Detection Rules when its flag is on", () => {
    const tabs = policyCenterTabs({ toolApprovals: true });
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "tool-approvals",
      "exclusions",
      "quarantines",
    ]);
    expect(tabs.find((tab) => tab.value === "tool-approvals")).toMatchObject({
      label: "Tool approvals",
      href: "?tab=tool-approvals",
    });
  });

  it("lands a link to a hidden tab on Policies", () => {
    expect(resolvePolicyCenterTab("tool-approvals")).toBe("policies");
    expect(
      resolvePolicyCenterTab("tool-approvals", { toolApprovals: true }),
    ).toBe("tool-approvals");
  });

  it("builds the search string a link lands on a tab with", () => {
    expect(policyCenterTabSearch("exclusions")).toBe("?tab=exclusions");
  });
});

describe("movedToShadowAITab", () => {
  // Slack, block messages and bookmarks still carry the old tab links.
  it("sends the old Shadow tabs to their Shadow AI tab", () => {
    expect(movedToShadowAITab("mcp-approvals")).toBe("mcp-servers");
    expect(movedToShadowAITab("shadow-ai-targets")).toBe("ai-tools");
  });

  it("leaves every other tab on Guardrails", () => {
    expect(movedToShadowAITab(null)).toBeUndefined();
    expect(movedToShadowAITab("policies")).toBeUndefined();
    expect(movedToShadowAITab("toString")).toBeUndefined();
  });
});

// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  movedToShadowMCP,
  policyCenterTabs,
  policyCenterTabSearch,
  resolvePolicyCenterTab,
} from "./policy-center-tabs";

describe("policyCenterTabs", () => {
  it("shows the Shadow AI targets tab after Detection Rules, and no MCP approvals", () => {
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

  it("shows Tool approvals after Shadow AI targets when its flag is on", () => {
    const tabs = policyCenterTabs({ toolApprovals: true });
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "shadow-ai-targets",
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
    expect(resolvePolicyCenterTab("exclusions")).toBe("exclusions");
  });

  it("builds the search string a link lands on a tab with", () => {
    expect(policyCenterTabSearch("exclusions")).toBe("?tab=exclusions");
  });
});

describe("movedToShadowMCP", () => {
  // Slack, block messages and bookmarks still carry the old tab link.
  it("sends the old MCP approvals tab to Shadow MCP", () => {
    expect(movedToShadowMCP("mcp-approvals")).toBe(true);
  });

  it("leaves every other tab on Guardrails", () => {
    expect(movedToShadowMCP(null)).toBe(false);
    expect(movedToShadowMCP("policies")).toBe(false);
    expect(movedToShadowMCP("shadow-ai-targets")).toBe(false);
  });
});

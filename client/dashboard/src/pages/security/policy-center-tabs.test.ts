import { describe, expect, it } from "vitest";
import {
  policyCenterTabs,
  policyCenterTabSearch,
  resolvePolicyCenterTab,
} from "./policy-center-tabs";

describe("policyCenterTabs", () => {
  it("shows the Shadow AI targets and MCP approvals tabs after Detection Rules", () => {
    const tabs = policyCenterTabs();
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "shadow-ai-targets",
      "mcp-approvals",
      "exclusions",
      "quarantines",
    ]);
    expect(tabs.find((tab) => tab.value === "shadow-ai-targets")).toMatchObject(
      {
        label: "Shadow AI targets",
        href: "?tab=shadow-ai-targets",
      },
    );
    expect(tabs.find((tab) => tab.value === "mcp-approvals")).toMatchObject({
      label: "MCP approvals",
      href: "?tab=mcp-approvals",
    });
  });

  it("shows Tool approvals after MCP approvals when its flag is on", () => {
    const tabs = policyCenterTabs({ toolApprovals: true });
    expect(tabs.map((tab) => tab.value)).toEqual([
      "policies",
      "detection-rules",
      "shadow-ai-targets",
      "mcp-approvals",
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
    expect(resolvePolicyCenterTab("mcp-approvals")).toBe("mcp-approvals");
  });

  it("builds the search string a link lands on a tab with", () => {
    expect(policyCenterTabSearch("mcp-approvals")).toBe("?tab=mcp-approvals");
  });
});

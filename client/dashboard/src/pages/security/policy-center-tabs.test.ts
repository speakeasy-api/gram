import { describe, expect, it } from "vitest";
import { policyCenterTabs, policyCenterTabSearch } from "./policy-center-tabs";

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

  it("builds the search string a link lands on a tab with", () => {
    expect(policyCenterTabSearch("mcp-approvals")).toBe("?tab=mcp-approvals");
  });
});

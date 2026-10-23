import { describe, expect, it } from "vitest";
import { serverTabForToolsetTab } from "./toolsetRedirectRouting";
import { tabSegmentFromPath } from "./x/MCPServerDetailsRouting";

describe("tabSegmentFromPath for toolset slugs", () => {
  it("reads the tab after the toolset slug", () => {
    expect(
      tabSegmentFromPath(
        "/acme/projects/default/mcp/my-tools/team-access",
        "my-tools",
      ),
    ).toBe("team-access");
  });

  it("reads no tab from the bare toolset URL", () => {
    expect(
      tabSegmentFromPath("/acme/projects/default/mcp/my-tools", "my-tools"),
    ).toBeUndefined();
  });

  it("matches a toolset slug that equals a tab name", () => {
    expect(
      tabSegmentFromPath(
        "/acme/projects/default/mcp/settings/tools",
        "settings",
      ),
    ).toBe("tools");
  });
});

describe("serverTabForToolsetTab", () => {
  it.each([
    [undefined, "", "overview", ""],
    ["overview", "", "overview", ""],
    ["tools", "", "tools", ""],
    ["resources", "", "resources", ""],
    ["prompts", "", "prompts", ""],
    ["team-access", "", "team-access", ""],
    ["sessions", "", "sessions", ""],
    ["settings", "", "settings", ""],
    ["settings", "#authentication", "settings", "#authentication"],
    ["authentication", "", "settings", "#authentication"],
    ["performance", "", "settings", "#performance"],
    [undefined, "#authentication", "settings", "#authentication"],
    [undefined, "#tools", "tools", ""],
    [undefined, "#environment", "settings", "#environment"],
    ["unknown", "", "overview", ""],
  ] as const)("maps %s%s to %s%s", (segment, hash, tab, targetHash) => {
    expect(serverTabForToolsetTab(segment, hash)).toEqual({
      tab,
      hash: targetHash,
    });
  });
});

import { describe, expect, it } from "vitest";
import {
  activeTabFromPath,
  initialTabFromHash,
  resolveTabForBackend,
  tabsForBackend,
} from "./MCPServerDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the server details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/x/overview", "overview"),
    ).toBeUndefined();
  });

  it.each(["overview", "inspect", "team-access", "settings"] as const)(
    "reads the %s tab when the server slug has the same value",
    (tab) => {
      expect(
        activeTabFromPath(`/acme/projects/default/mcp/x/${tab}/${tab}`, tab),
      ).toBe(tab);
    },
  );

  it("reads the tab segment after the matching server slug", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/overview/settings",
        "overview",
      ),
    ).toBe("settings");
  });

  it("ignores route segments before x/:mcpServerSlug", () => {
    expect(
      activeTabFromPath(
        "/overview/projects/default/mcp/x/default/settings",
        "default",
      ),
    ).toBe("settings");
  });

  it("matches the mcp/x route marker instead of any x-prefixed segment", () => {
    expect(
      activeTabFromPath("/acme/projects/x/mcp/x/mcp/settings", "mcp"),
    ).toBe("settings");
  });

  it.each([
    "tools",
    "resources",
    "prompts",
    "authentication",
    "performance",
  ] as const)("reads the toolset-backed %s tab", (tab) => {
    expect(
      activeTabFromPath(
        `/acme/projects/default/mcp/x/my-server/${tab}`,
        "my-server",
      ),
    ).toBe(tab);
  });

  it("matches decoded server slug segments", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my%20server/settings",
        "my server",
      ),
    ).toBe("settings");
  });

  it("returns no tab for an invalid tab segment", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my-server/nope",
        "my-server",
      ),
    ).toBeUndefined();
  });
});

describe("resolveTabForBackend", () => {
  it("folds authentication into settings for source-backed servers", () => {
    expect(resolveTabForBackend("authentication", false)).toEqual({
      tab: "settings",
      hash: "authentication",
    });
  });

  it("folds tools into inspect for source-backed servers", () => {
    expect(resolveTabForBackend("tools", false)).toEqual({ tab: "inspect" });
  });

  it.each(["resources", "prompts", "performance"] as const)(
    "folds %s into overview for source-backed servers",
    (tab) => {
      expect(resolveTabForBackend(tab, false)).toEqual({ tab: "overview" });
    },
  );

  it("keeps toolset-only tabs for toolset-backed servers", () => {
    expect(resolveTabForBackend("tools", true)).toEqual({ tab: "tools" });
    expect(resolveTabForBackend("authentication", true)).toEqual({
      tab: "authentication",
    });
  });

  it("folds inspect into tools for toolset-backed servers", () => {
    expect(resolveTabForBackend("inspect", true)).toEqual({ tab: "tools" });
  });

  it("keeps shared tabs for both backend kinds", () => {
    expect(resolveTabForBackend("settings", true)).toEqual({ tab: "settings" });
    expect(resolveTabForBackend("settings", false)).toEqual({
      tab: "settings",
    });
  });
});

describe("tabsForBackend", () => {
  it("excludes inspect for toolset-backed servers", () => {
    expect(tabsForBackend(true)).not.toContain("inspect");
    expect(tabsForBackend(true)).toContain("tools");
    expect(tabsForBackend(true)).toContain("prompts");
  });

  it("excludes toolset-only tabs for source-backed servers", () => {
    expect(tabsForBackend(false)).toEqual([
      "overview",
      "inspect",
      "team-access",
      "settings",
    ]);
  });
});

describe("initialTabFromHash", () => {
  it("maps the authentication hash to settings for source-backed servers", () => {
    expect(initialTabFromHash("#authentication", false)).toBe("settings");
  });

  it("maps the authentication hash to the authentication tab for toolset-backed servers", () => {
    expect(initialTabFromHash("#authentication", true)).toBe("authentication");
  });

  it("maps the tools hash to inspect for source-backed servers", () => {
    expect(initialTabFromHash("#tools", false)).toBe("inspect");
  });

  it("supports team access", () => {
    expect(initialTabFromHash("#team-access", false)).toBe("team-access");
  });

  it("defaults to overview", () => {
    expect(initialTabFromHash("", true)).toBe("overview");
    expect(initialTabFromHash("#nope", false)).toBe("overview");
  });
});

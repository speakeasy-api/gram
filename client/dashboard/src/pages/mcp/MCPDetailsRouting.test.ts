import { describe, expect, it } from "vitest";
import { activeTabFromPath, initialTabFromHash } from "./MCPDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/grain-copy", "grain-copy"),
    ).toBeUndefined();
  });

  it.each(["overview", "team-access", "settings"] as const)(
    "reads the %s tab when the toolset slug has the same value",
    (tab) => {
      expect(
        activeTabFromPath(`/acme/projects/default/mcp/${tab}/${tab}`, tab),
      ).toBe(tab);
    },
  );

  it("reads the tab segment after the matching toolset slug", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/grain-copy/settings",
        "grain-copy",
      ),
    ).toBe("settings");
  });

  it("ignores route segments before mcp/:toolsetSlug", () => {
    expect(
      activeTabFromPath(
        "/overview/projects/default/mcp/default/settings",
        "default",
      ),
    ).toBe("settings");
  });

  it("matches decoded toolset slug segments", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/my%20toolset/settings",
        "my toolset",
      ),
    ).toBe("settings");
  });

  it("returns no tab for an invalid tab segment", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/grain-copy/nope",
        "grain-copy",
      ),
    ).toBeUndefined();
  });
});

describe("initialTabFromHash", () => {
  it("maps an unknown hash to overview", () => {
    expect(initialTabFromHash("#nope", true)).toBe("overview");
  });

  it("keeps team access behind the RBAC feature flag", () => {
    expect(initialTabFromHash("#team-access", false)).toBe("overview");
    expect(initialTabFromHash("#team-access", true)).toBe("team-access");
  });

  it("reads a valid hash directly", () => {
    expect(initialTabFromHash("#authentication", true)).toBe("authentication");
  });
});

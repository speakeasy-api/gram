import { describe, expect, it } from "vitest";
import { activeTabFromPath } from "./MCPServerDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the server details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/x/overview", "overview"),
    ).toBeUndefined();
  });

  it.each(["overview", "authentication", "team-access", "settings"] as const)(
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

  it("matches decoded server slug segments", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my%20server/authentication",
        "my server",
      ),
    ).toBe("authentication");
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

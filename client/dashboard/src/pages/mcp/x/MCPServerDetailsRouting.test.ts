import { describe, expect, it } from "vitest";
import {
  activeTabFromPath,
  initialTabFromHash,
  isLegacyAuthenticationTabPath,
} from "./MCPServerDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the server details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/x/overview", "overview"),
    ).toBeUndefined();
  });

  it.each(["overview", "team-access", "settings"] as const)(
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

  it("does not treat the legacy authentication path as an active tab", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my-server/authentication",
        "my-server",
      ),
    ).toBeUndefined();
  });

  it("detects the legacy authentication path for redirects", () => {
    expect(
      isLegacyAuthenticationTabPath(
        "/acme/projects/default/mcp/x/my-server/authentication",
        "my-server",
      ),
    ).toBe(true);
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

describe("initialTabFromHash", () => {
  it("maps the legacy authentication hash to settings", () => {
    expect(initialTabFromHash("#authentication", true)).toBe("settings");
  });

  it("keeps team access behind the RBAC feature flag", () => {
    expect(initialTabFromHash("#team-access", false)).toBe("overview");
    expect(initialTabFromHash("#team-access", true)).toBe("team-access");
  });
});

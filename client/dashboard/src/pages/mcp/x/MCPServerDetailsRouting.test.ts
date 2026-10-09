import { describe, expect, it } from "vitest";
import {
  activeTabFromPath,
  initialTabFromHash,
  isCanonicalHostedWrapper,
  isLegacyAuthenticationTabPath,
  isLegacyToolsTabPath,
  toolsetTabForServerPath,
} from "./MCPServerDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the server details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/x/overview", "overview"),
    ).toBeUndefined();
  });

  it.each([
    "overview",
    "inspect",
    "team-access",
    "guardrails",
    "settings",
  ] as const)(
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

  it("does not treat the legacy tools path as an active tab", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my-server/tools",
        "my-server",
      ),
    ).toBeUndefined();
  });

  it("detects the legacy tools path for redirects", () => {
    expect(
      isLegacyToolsTabPath(
        "/acme/projects/default/mcp/x/my-server/tools",
        "my-server",
      ),
    ).toBe(true);
  });

  it("does not confuse the inspect tab with the legacy tools path", () => {
    expect(
      isLegacyToolsTabPath(
        "/acme/projects/default/mcp/x/my-server/inspect",
        "my-server",
      ),
    ).toBe(false);
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
    expect(initialTabFromHash("#authentication")).toBe("settings");
  });

  it("maps the legacy tools hash to inspect", () => {
    expect(initialTabFromHash("#tools")).toBe("inspect");
  });

  it("supports team access", () => {
    expect(initialTabFromHash("#team-access")).toBe("team-access");
  });
});

describe("isCanonicalHostedWrapper", () => {
  const toolsetId = "7f1c2a9e-1111-4c4c-9a9a-000000000001";

  it("treats a server whose id is its toolset id as the canonical wrapper", () => {
    expect(isCanonicalHostedWrapper({ id: toolsetId, toolsetId })).toBe(true);
  });

  it("keeps a fresh-id toolset-backed server on the server page", () => {
    expect(
      isCanonicalHostedWrapper({
        id: "7f1c2a9e-2222-4c4c-9a9a-000000000002",
        toolsetId,
      }),
    ).toBe(false);
  });

  it("keeps a remote server on the server page", () => {
    expect(
      isCanonicalHostedWrapper({
        id: "7f1c2a9e-3333-4c4c-9a9a-000000000003",
        toolsetId: undefined,
      }),
    ).toBe(false);
  });
});

describe("toolsetTabForServerPath", () => {
  const base = "/acme/projects/default/mcp/x/my-server";

  it.each([
    ["", "", "overview"],
    ["/overview", "", "overview"],
    ["/inspect", "", "tools"],
    ["/tools", "", "tools"],
    ["/authentication", "", "authentication"],
    ["/team-access", "", "team-access"],
    ["/guardrails", "", undefined],
    ["/sessions", "", "sessions"],
    ["/settings", "", "settings"],
    ["/settings", "#authentication", "authentication"],
    ["", "#authentication", "authentication"],
    ["", "#tools", "tools"],
  ] as const)("maps %s%s to toolset tab %s", (suffix, hash, expected) => {
    expect(toolsetTabForServerPath(`${base}${suffix}`, "my-server", hash)).toBe(
      expected,
    );
  });
});

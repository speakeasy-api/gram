import { describe, expect, it } from "vitest";
import {
  activeTabFromPath,
  initialTabFromHash,
  isCanonicalHostedWrapper,
  standsInForToolset,
  tabForServer,
  tabSegmentFromPath,
  withoutMcpXSegment,
} from "./MCPServerDetailsRouting";

describe("activeTabFromPath", () => {
  it("returns no tab for the server details route without a tab segment", () => {
    expect(
      activeTabFromPath("/acme/projects/default/mcp/overview", "overview"),
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
        activeTabFromPath(`/acme/projects/default/mcp/${tab}/${tab}`, tab),
      ).toBe(tab);
    },
  );

  it("reads the tab by position, not by searching for mcp", () => {
    expect(activeTabFromPath("/mcp/projects/mcp/mcp/mcp/settings", "mcp")).toBe(
      "settings",
    );
    expect(
      activeTabFromPath(
        "/overview/projects/default/mcp/default/settings",
        "default",
      ),
    ).toBe("settings");
  });

  it("reads a server slugged x like any other", () => {
    expect(activeTabFromPath("/acme/projects/x/mcp/x/inspect", "x")).toBe(
      "inspect",
    );
  });

  it("returns no tab when the slug is not the server segment", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/x/my-server/settings",
        "my-server",
      ),
    ).toBeUndefined();
  });

  it("does not treat a retired tab segment as an active tab", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/my-server/authentication",
        "my-server",
      ),
    ).toBeUndefined();
    expect(
      tabSegmentFromPath(
        "/acme/projects/default/mcp/my-server/authentication",
        "my-server",
      ),
    ).toBe("authentication");
  });

  it("matches decoded server slug segments", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/my%20server/settings",
        "my server",
      ),
    ).toBe("settings");
  });

  it("returns no tab for an invalid tab segment", () => {
    expect(
      activeTabFromPath(
        "/acme/projects/default/mcp/my-server/nope",
        "my-server",
      ),
    ).toBeUndefined();
  });
});

describe("withoutMcpXSegment", () => {
  it.each([
    [
      "/acme/projects/default/mcp/x/my-server",
      "/acme/projects/default/mcp/my-server",
    ],
    [
      "/acme/projects/default/mcp/x/my-server/settings",
      "/acme/projects/default/mcp/my-server/settings",
    ],
    [
      "/acme/projects/default/mcp/x/my%20server/inspect",
      "/acme/projects/default/mcp/my%20server/inspect",
    ],
    ["/acme/projects/x/mcp/x/x/overview", "/acme/projects/x/mcp/x/overview"],
  ])("rewrites %s", (from, to) => {
    expect(withoutMcpXSegment(from)).toBe(to);
  });

  it.each([
    "/acme/projects/default/mcp/x",
    "/acme/projects/default/mcp/my-server/x",
    "/acme/projects/x/insights",
  ])("leaves %s alone", (path) => {
    expect(withoutMcpXSegment(path)).toBeUndefined();
  });
});

describe("initialTabFromHash", () => {
  it("maps the legacy authentication hash to settings", () => {
    expect(initialTabFromHash("#authentication")).toBe("settings");
  });

  it("reads the tools hash as the tools tab", () => {
    expect(initialTabFromHash("#tools")).toBe("tools");
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

describe("standsInForToolset", () => {
  const toolsetId = "7f1c2a9e-1111-4c4c-9a9a-000000000001";
  const freshId = "7f1c2a9e-2222-4c4c-9a9a-000000000002";

  it("treats the server the API names as the toolset's host as hosted", () => {
    expect(
      standsInForToolset({
        id: freshId,
        toolsetId,
        toolset: { hostingMcpServerId: freshId },
      }),
    ).toBe(true);
  });

  it("leaves a server beside the toolset's host unhosted", () => {
    expect(
      standsInForToolset({
        id: freshId,
        toolsetId,
        toolset: { hostingMcpServerId: toolsetId },
      }),
    ).toBe(false);
  });

  it("falls back to the canonical rule when the API names no host", () => {
    expect(standsInForToolset({ id: toolsetId, toolsetId, toolset: {} })).toBe(
      true,
    );
    expect(standsInForToolset({ id: freshId, toolsetId })).toBe(false);
  });

  it("never treats a server without a toolset as hosted", () => {
    expect(
      standsInForToolset({
        id: freshId,
        toolsetId: undefined,
        toolset: { hostingMcpServerId: freshId },
      }),
    ).toBe(false);
  });
});

describe("tabForServer", () => {
  it("shows a hosted server's tools instead of Inspect", () => {
    expect(tabForServer("inspect", true)).toBe("tools");
    for (const tab of ["tools", "resources", "prompts", "settings"] as const) {
      expect(tabForServer(tab, true)).toBe(tab);
    }
  });

  it("keeps the old tools URL of any other server on Inspect", () => {
    expect(tabForServer("tools", false)).toBe("inspect");
    expect(tabForServer("resources", false)).toBe("overview");
    expect(tabForServer("prompts", false)).toBe("overview");
    expect(tabForServer("inspect", false)).toBe("inspect");
  });
});

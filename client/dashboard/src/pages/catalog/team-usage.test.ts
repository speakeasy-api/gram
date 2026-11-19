import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import type { CatalogServer } from "./hooks";
import { catalogTeamUsage, teamUsageLabel } from "./team-usage";

function entry(registrySpecifier: string, urls: string[]): CatalogServer {
  return {
    registrySpecifier,
    description: "",
    version: "1.0.0",
    meta: {},
    supportsDcr: true,
    remotes: urls.map((url) => ({ url, transportType: "streamable-http" })),
  } as unknown as CatalogServer;
}

function shadow(
  overrides: Partial<ShadowMCPInventoryServer>,
): ShadowMCPInventoryServer {
  return {
    canonicalServerUrl: "https://mcp.example.com/mcp",
    serverSlug: "mcp-example-com",
    urlHost: "mcp.example.com",
    firstSeen: "2026-11-01T00:00:00Z",
    lastSeen: "2026-11-18T00:00:00Z",
    observedUseCount: 1,
    userCount: 1,
    topUsers: [],
    access: "none",
    requestCount: 0,
    allowedPolicyIds: [],
    blockedPolicyIds: [],
    ...overrides,
  } as ShadowMCPInventoryServer;
}

describe("catalogTeamUsage", () => {
  const catalog = [
    entry("app.linear/linear", [
      "https://mcp.linear.app/mcp",
      "https://mcp.linear.app/sse",
    ]),
    entry("com.notion/notion", ["https://mcp.notion.com/mcp"]),
  ];

  it("matches inventory URLs to catalog entries the way Shadow MCP compares them", () => {
    const usage = catalogTeamUsage(catalog, [
      shadow({
        canonicalServerUrl: "https://MCP.notion.com:443/mcp/",
        observedUseCount: 36,
        userCount: 5,
      }),
    ]);
    expect(usage.get("com.notion/notion")).toEqual({
      userCount: 5,
      callCount: 36,
    });
  });

  it("adds calls across an entry's URLs but not people", () => {
    const usage = catalogTeamUsage(catalog, [
      shadow({
        canonicalServerUrl: "https://mcp.linear.app/mcp",
        observedUseCount: 20,
        userCount: 4,
      }),
      shadow({
        canonicalServerUrl: "https://mcp.linear.app/sse",
        observedUseCount: 3,
        userCount: 2,
      }),
    ]);
    expect(usage.get("app.linear/linear")).toEqual({
      userCount: 4,
      callCount: 23,
    });
  });

  it("ignores local commands, unused rows and URLs outside the catalog", () => {
    const usage = catalogTeamUsage(catalog, [
      shadow({
        targetKind: "stdio_command",
        canonicalServerUrl: "https://mcp.notion.com/mcp",
        observedUseCount: 9,
      }),
      shadow({
        canonicalServerUrl: "https://mcp.linear.app/mcp",
        observedUseCount: 0,
        userCount: 0,
      }),
      shadow({
        canonicalServerUrl: "https://tools.example.com/mcp",
        observedUseCount: 50,
      }),
    ]);
    expect(usage.size).toBe(0);
  });
});

describe("teamUsageLabel", () => {
  it("names people and calls", () => {
    expect(teamUsageLabel({ userCount: 5, callCount: 1234 })).toBe(
      "Used by 5 people · 1,234 calls",
    );
    expect(teamUsageLabel({ userCount: 1, callCount: 1 })).toBe(
      "Used by 1 person · 1 call",
    );
  });
});

describe("catalogTeamUsage shared URLs", () => {
  it("credits every catalog entry that serves the URL", () => {
    const usage = catalogTeamUsage(
      [
        entry("io.github/github", ["https://api.githubcopilot.com/mcp"]),
        entry("com.github/copilot", ["https://api.githubcopilot.com/mcp"]),
      ],
      [
        shadow({
          canonicalServerUrl: "https://api.githubcopilot.com/mcp",
          observedUseCount: 51,
          userCount: 5,
        }),
      ],
    );
    expect(usage.get("io.github/github")).toEqual({
      userCount: 5,
      callCount: 51,
    });
    expect(usage.get("com.github/copilot")).toEqual({
      userCount: 5,
      callCount: 51,
    });
  });
});

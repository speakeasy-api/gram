// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  catalogInstances,
  catalogServerInstallState,
  type CatalogInstallEvidence,
  type CatalogInstallReady,
} from "./catalogInstall";

const github = {
  registrySpecifier: "io.github.github/github-mcp-server",
  remotes: [{ url: "https://api.githubcopilot.com/mcp/" }],
};

const emptyEvidence: CatalogInstallEvidence = {
  remoteUrls: new Set(),
  unproxiedUrls: new Set(),
  originSpecifiers: new Set(),
};

const allReady: CatalogInstallReady = {
  remote: true,
  unproxied: true,
  servers: true,
};

function stateFor(
  evidence: Partial<CatalogInstallEvidence>,
  ready: Partial<CatalogInstallReady> = {},
) {
  return catalogServerInstallState(
    github,
    { ...emptyEvidence, ...evidence },
    { ...allReady, ...ready },
  );
}

describe("catalogServerInstallState", () => {
  it("matches a remote URL before the other lookups finish", () => {
    expect(
      stateFor(
        { remoteUrls: new Set(["https://api.githubcopilot.com/mcp"]) },
        { unproxied: false, servers: false },
      ),
    ).toBe("added");
  });

  it("stays added when any endpoint is still installed", () => {
    expect(
      catalogServerInstallState(
        {
          registrySpecifier: "com.example/multi",
          remotes: [
            { url: "https://mcp.example.com/a" },
            { url: "https://mcp.example.com/b" },
          ],
        },
        {
          ...emptyEvidence,
          remoteUrls: new Set(["https://mcp.example.com/b"]),
        },
        allReady,
      ),
    ).toBe("added");
  });

  it("matches an unproxied URL", () => {
    expect(
      stateFor(
        { unproxiedUrls: new Set(["https://api.githubcopilot.com/mcp"]) },
        { remote: false },
      ),
    ).toBe("added");
  });

  it("stays unknown until every lookup has data", () => {
    expect(stateFor({}, { remote: false })).toBe("unknown");
    expect(stateFor({}, { unproxied: false })).toBe("unknown");
    expect(stateFor({}, { servers: false })).toBe("unknown");
  });

  it("is not-added only after every lookup succeeded with no match", () => {
    expect(stateFor({})).toBe("not-added");
  });

  it("treats a server catalog origin as installed", () => {
    expect(
      stateFor(
        { originSpecifiers: new Set([github.registrySpecifier]) },
        { remote: false, unproxied: false },
      ),
    ).toBe("added");
  });

  it("does not match a different catalog entry", () => {
    expect(
      stateFor({
        remoteUrls: new Set(["https://mcp.other.example/mcp"]),
        originSpecifiers: new Set(["com.other/server"]),
      }),
    ).toBe("not-added");
  });
});

describe("catalogInstances", () => {
  const noUrls = new Map<string, string>();

  it("matches a remote server by endpoint URL", () => {
    const remote = { id: "a", remoteMcpServerId: "r1" };
    const other = { id: "b", remoteMcpServerId: "r2" };
    const urls = new Map([
      ["r1", "https://api.githubcopilot.com/mcp"],
      ["r2", "https://example.com/mcp"],
    ]);
    expect(catalogInstances(github, [remote, other], urls, noUrls)).toEqual([
      remote,
    ]);
  });

  it("matches an unproxied server by endpoint URL", () => {
    const unproxied = { id: "a", unproxiedMcpServerId: "u1" };
    const urls = new Map([["u1", "https://api.githubcopilot.com/mcp/"]]);
    expect(catalogInstances(github, [unproxied], noUrls, urls)).toEqual([
      unproxied,
    ]);
  });

  it("matches an older install by toolset origin", () => {
    const origin = {
      id: "a",
      toolset: { registrySpecifier: github.registrySpecifier },
    };
    const unrelated = { id: "b", toolset: { registrySpecifier: "other/mcp" } };
    expect(
      catalogInstances(github, [origin, unrelated], noUrls, noUrls),
    ).toEqual([origin]);
  });

  it("does not match a server whose remote is no longer listed", () => {
    const orphan = { id: "a", remoteMcpServerId: "gone" };
    expect(catalogInstances(github, [orphan], noUrls, noUrls)).toEqual([]);
  });
});

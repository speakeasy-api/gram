// @vitest-environment node
import { describe, expect, it } from "vitest";
import {
  catalogServerInstallState,
  externalMcpSlugFromToolUrn,
  liveCatalogExternalMcp,
  type CatalogInstallEvidence,
  type CatalogInstallReady,
} from "./catalogInstall";

const github = {
  registrySpecifier: "io.github.github/github-mcp-server",
  remotes: [{ url: "https://api.githubcopilot.com/mcp/" }],
};

const attachment = {
  slug: "github",
  registryServerSpecifier: github.registrySpecifier,
};

const emptyEvidence: CatalogInstallEvidence = {
  remoteUrls: new Set(),
  unproxiedUrls: new Set(),
  originSpecifiers: new Set(),
  referencedExternalMcpSlugs: new Set(),
};

const allReady: CatalogInstallReady = {
  remote: true,
  unproxied: true,
  servers: true,
  deployment: true,
};

function stateFor(
  evidence: Partial<CatalogInstallEvidence>,
  ready: Partial<CatalogInstallReady> = {},
  externalMcps: (typeof attachment)[] = [],
) {
  return catalogServerInstallState(
    github,
    { ...emptyEvidence, ...evidence },
    externalMcps,
    { ...allReady, ...ready },
  );
}

describe("externalMcpSlugFromToolUrn", () => {
  it("reads the source slug from any tool name", () => {
    expect(externalMcpSlugFromToolUrn("tools:externalmcp:github:proxy")).toBe(
      "github",
    );
    expect(
      externalMcpSlugFromToolUrn("tools:externalmcp:github:listRepos"),
    ).toBe("github");
  });

  it("ignores other tool kinds", () => {
    expect(
      externalMcpSlugFromToolUrn("tools:http:github:proxy"),
    ).toBeUndefined();
  });
});

describe("catalogServerInstallState", () => {
  it("matches a remote URL before the other lookups finish", () => {
    expect(
      stateFor(
        { remoteUrls: new Set(["https://api.githubcopilot.com/mcp"]) },
        { unproxied: false, servers: false, deployment: false },
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
        [],
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
    expect(stateFor({}, { servers: false })).toBe("unknown");
    expect(stateFor({}, { deployment: false })).toBe("unknown");
  });

  it("is not-added only after every lookup succeeded with no match", () => {
    expect(stateFor({})).toBe("not-added");
  });

  it("does not treat a deployment attachment with no server as installed", () => {
    expect(stateFor({}, {}, [attachment])).toBe("not-added");
    expect(
      liveCatalogExternalMcp(github, [attachment], new Set()),
    ).toBeUndefined();
  });

  it("treats an attachment as installed when a server still selects its slug", () => {
    expect(
      stateFor({ referencedExternalMcpSlugs: new Set(["github"]) }, {}, [
        attachment,
      ]),
    ).toBe("added");
  });

  it("treats a server catalog origin as installed without an attachment", () => {
    expect(
      stateFor(
        { originSpecifiers: new Set([github.registrySpecifier]) },
        { deployment: false },
      ),
    ).toBe("added");
  });

  it("does not match a different catalog entry", () => {
    expect(
      stateFor({
        remoteUrls: new Set(["https://mcp.other.example/mcp"]),
        originSpecifiers: new Set(["com.other/server"]),
        referencedExternalMcpSlugs: new Set(["other"]),
      }),
    ).toBe("not-added");
  });
});

import { describe, expect, it } from "vitest";
import {
  catalogServerIsInstalled,
  collectCatalogInstallEvidence,
  externalMcpSlugFromToolUrn,
  liveCatalogExternalMcp,
} from "./catalogInstall";

const github = {
  registrySpecifier: "io.github.github/github-mcp-server",
  remotes: [{ url: "https://api.githubcopilot.com/mcp/" }],
};

const attachment = {
  slug: "github",
  registryServerSpecifier: github.registrySpecifier,
};

function evidenceFor(input: {
  urls?: string[];
  toolsets?: {
    origin?: { registrySpecifier?: string };
    toolUrns?: string[];
  }[];
}) {
  return collectCatalogInstallEvidence({
    urls: input.urls ?? [],
    toolsets: input.toolsets ?? [],
  });
}

describe("externalMcpSlugFromToolUrn", () => {
  it("reads the source slug", () => {
    expect(externalMcpSlugFromToolUrn("tools:externalmcp:github:proxy")).toBe(
      "github",
    );
  });

  it("ignores other tool kinds", () => {
    expect(
      externalMcpSlugFromToolUrn("tools:http:github:proxy"),
    ).toBeUndefined();
  });
});

describe("catalogServerIsInstalled", () => {
  it("matches a remote or unproxied server URL, ignoring a trailing slash", () => {
    const evidence = evidenceFor({
      urls: ["https://api.githubcopilot.com/mcp"],
    });
    expect(catalogServerIsInstalled(github, evidence, [])).toBe(true);
  });

  it("stays installed when another endpoint URL is still present", () => {
    const server = {
      registrySpecifier: "com.example/multi",
      remotes: [
        { url: "https://mcp.example.com/a" },
        { url: "https://mcp.example.com/b" },
      ],
    };
    const evidence = evidenceFor({
      urls: ["https://mcp.example.com/b"],
    });
    expect(catalogServerIsInstalled(server, evidence, [])).toBe(true);
  });

  it("does not treat a deployment attachment with no toolset as installed", () => {
    const evidence = evidenceFor({});
    expect(catalogServerIsInstalled(github, evidence, [attachment])).toBe(
      false,
    );
    expect(
      liveCatalogExternalMcp(
        github,
        [attachment],
        evidence.referencedExternalMcpSlugs,
      ),
    ).toBeUndefined();
  });

  it("treats an attachment as installed when a toolset still selects it", () => {
    const evidence = evidenceFor({
      toolsets: [{ toolUrns: ["tools:externalmcp:github:proxy"] }],
    });
    expect(catalogServerIsInstalled(github, evidence, [attachment])).toBe(true);
    expect(
      liveCatalogExternalMcp(
        github,
        [attachment],
        evidence.referencedExternalMcpSlugs,
      )?.slug,
    ).toBe("github");
  });

  it("treats a toolset catalog origin as installed after the attachment is gone", () => {
    const evidence = evidenceFor({
      toolsets: [{ origin: { registrySpecifier: github.registrySpecifier } }],
    });
    expect(catalogServerIsInstalled(github, evidence, [])).toBe(true);
  });

  it("does not match a different catalog entry", () => {
    const evidence = evidenceFor({
      urls: ["https://mcp.other.example/mcp"],
      toolsets: [
        {
          origin: { registrySpecifier: "com.other/server" },
          toolUrns: ["tools:externalmcp:other:proxy"],
        },
      ],
    });
    expect(
      catalogServerIsInstalled(github, evidence, [
        { slug: "other", registryServerSpecifier: "com.other/server" },
      ]),
    ).toBe(false);
  });
});

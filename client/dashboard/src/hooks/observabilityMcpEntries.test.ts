import { describe, expect, it } from "vitest";
import { observabilityMcpEntries } from "./observabilityMcpEntries";

const base = {
  projectSlug: "proj",
  serverURL: "https://gram.test",
  toolsetsLoading: false,
  toolsets: [] as const,
  mcpServersLoading: false,
  mcpServers: [] as const,
  endpointsLoading: false,
  endpoints: [] as const,
};

describe("observabilityMcpEntries", () => {
  it("is unknown while any listing is loading or missing", () => {
    expect(
      observabilityMcpEntries({ ...base, toolsetsLoading: true }),
    ).toBeUndefined();
    expect(
      observabilityMcpEntries({ ...base, mcpServersLoading: true }),
    ).toBeUndefined();
    expect(
      observabilityMcpEntries({ ...base, endpointsLoading: true }),
    ).toBeUndefined();
    expect(
      observabilityMcpEntries({ ...base, toolsets: undefined }),
    ).toBeUndefined();
    expect(
      observabilityMcpEntries({ ...base, mcpServers: undefined }),
    ).toBeUndefined();
    expect(
      observabilityMcpEntries({ ...base, endpoints: undefined }),
    ).toBeUndefined();
  });

  it("is an empty list when every listing settled empty", () => {
    expect(observabilityMcpEntries(base)).toEqual([]);
  });

  it("includes toolset-backed URLs", () => {
    expect(
      observabilityMcpEntries({
        ...base,
        toolsets: [
          {
            slug: "github",
            mcpSlug: "github-mcp",
            defaultEnvironmentSlug: "default",
          },
        ],
      }),
    ).toEqual([
      {
        url: "https://gram.test/mcp/github-mcp",
        name: "github",
        environment: "default",
      },
    ]);
  });

  it("includes directly hosted MCP servers that have an endpoint", () => {
    expect(
      observabilityMcpEntries({
        ...base,
        mcpServers: [
          { id: "srv-1", slug: "remote-docs", visibility: "private" },
        ],
        endpoints: [{ slug: "acme-remote-docs", mcpServerId: "srv-1" }],
      }),
    ).toEqual([
      {
        url: "https://gram.test/mcp/acme-remote-docs",
        name: "remote-docs",
      },
    ]);
  });

  it("skips custom-domain-only endpoints", () => {
    expect(
      observabilityMcpEntries({
        ...base,
        mcpServers: [
          { id: "custom-only", slug: "branded", visibility: "private" },
          { id: "platform", slug: "docs", visibility: "private" },
        ],
        endpoints: [
          {
            slug: "branded",
            mcpServerId: "custom-only",
            customDomainId: "dom-1",
          },
          { slug: "acme-docs", mcpServerId: "platform" },
        ],
      }),
    ).toEqual([
      {
        url: "https://gram.test/mcp/acme-docs",
        name: "docs",
      },
    ]);
  });

  it("skips disabled and unproxied servers and does not duplicate toolset URLs", () => {
    expect(
      observabilityMcpEntries({
        ...base,
        toolsets: [{ slug: "github", mcpSlug: "github-mcp" }],
        mcpServers: [
          { id: "toolset-srv", slug: "github", visibility: "private" },
          { id: "off", slug: "off", visibility: "disabled" },
          {
            id: "unproxied",
            slug: "vendor",
            visibility: "private",
            unproxiedMcpServerId: "up-1",
          },
        ],
        endpoints: [
          { slug: "github-mcp", mcpServerId: "toolset-srv" },
          { slug: "off-ep", mcpServerId: "off" },
          { slug: "vendor-ep", mcpServerId: "unproxied" },
        ],
      }),
    ).toEqual([
      {
        url: "https://gram.test/mcp/github-mcp",
        name: "github",
        environment: undefined,
      },
    ]);
  });
});

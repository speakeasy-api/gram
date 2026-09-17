import { describe, expect, it } from "vitest";
import type { Deployment } from "@gram/client/models/components/deployment.js";
import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import type { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";
import type { ToolsetEntry } from "@gram/client/models/components/toolsetentry.js";
import type { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";
import type { UnproxiedMcpServer } from "@gram/client/models/components/unproxiedmcpserver.js";
import {
  legacySourceKindLookups,
  parseLegacySourceKind,
  resolveLegacySourceRedirect,
  type LegacySourceLookup,
} from "./legacySourceRedirect";

const deployment = {
  id: "dep-1",
  openapiv3Assets: [
    { id: "doc-1", assetId: "asset-1", name: "Petstore", slug: "petstore" },
  ],
  functionsAssets: [
    {
      id: "fn-1",
      assetId: "asset-2",
      name: "Greeter",
      slug: "greeter",
      runtime: "nodejs:22",
    },
  ],
  externalMcps: [],
} as unknown as Deployment;

const mcpServers = [
  { id: "srv-remote", slug: "acme-remote", remoteMcpServerId: "remote-1" },
  { id: "srv-tunnel", slug: "", tunneledMcpServerId: "tunnel-1" },
  { id: "srv-unproxied", slug: "vendor", unproxiedMcpServerId: "unproxied-1" },
] as McpServer[];

const lookup: LegacySourceLookup = {
  deployment,
  mcpServers,
  remoteMcpServers: [
    { id: "remote-1", slug: "acme-mcp", url: "https://acme.example/mcp" },
  ] as RemoteMcpServer[],
  tunneledMcpServers: [
    { id: "tunnel-1", name: "Tunnel" },
  ] as TunneledMcpServer[],
  unproxiedMcpServers: [
    { id: "unproxied-1", slug: "vendor-mcp", url: "https://vendor.example" },
  ] as UnproxiedMcpServer[],
  toolsets: [
    {
      slug: "notion-server",
      toolUrns: ["tools:externalmcp:notion:search", "tools:http:petstore:list"],
    },
    { slug: "proxy-only", toolUrns: ["tools:externalmcp:linear:proxy"] },
  ] as ToolsetEntry[],
};

describe("parseLegacySourceKind", () => {
  it("accepts the retired route's kinds and rejects everything else", () => {
    expect(parseLegacySourceKind("http")).toBe("http");
    expect(parseLegacySourceKind("tunneledmcp")).toBe("tunneledmcp");
    expect(parseLegacySourceKind("prompt")).toBeUndefined();
    expect(parseLegacySourceKind(undefined)).toBeUndefined();
  });
});

describe("legacySourceKindLookups", () => {
  it("fetches only what each kind resolves against", () => {
    expect(legacySourceKindLookups("openapi")).toEqual({
      deployment: true,
      mcpServers: false,
      toolsets: false,
    });
    expect(legacySourceKindLookups("remotemcp")).toEqual({
      deployment: false,
      mcpServers: true,
      toolsets: false,
    });
    expect(legacySourceKindLookups("externalmcp")).toEqual({
      deployment: false,
      mcpServers: false,
      toolsets: true,
    });
    expect(legacySourceKindLookups(undefined)).toEqual({
      deployment: false,
      mcpServers: false,
      toolsets: false,
    });
  });
});

describe("resolveLegacySourceRedirect", () => {
  it("sends OpenAPI and function slugs to the source page by asset id", () => {
    expect(resolveLegacySourceRedirect("openapi", "petstore", lookup)).toEqual({
      kind: "source",
      assetId: "doc-1",
    });
    expect(resolveLegacySourceRedirect("http", "petstore", lookup)).toEqual({
      kind: "source",
      assetId: "doc-1",
    });
    expect(resolveLegacySourceRedirect("function", "greeter", lookup)).toEqual({
      kind: "source",
      assetId: "fn-1",
    });
  });

  it("falls back to the sources list when no asset carries the slug", () => {
    expect(resolveLegacySourceRedirect("openapi", "missing", lookup)).toEqual({
      kind: "sources-list",
    });
    expect(
      resolveLegacySourceRedirect("function", "greeter", {
        ...lookup,
        deployment: undefined,
      }),
    ).toEqual({ kind: "sources-list" });
  });

  it("resolves a remote source by slug or id to the server fronting it", () => {
    expect(
      resolveLegacySourceRedirect("remotemcp", "acme-mcp", lookup),
    ).toEqual({ kind: "mcp-server", routeParam: "acme-remote" });
    expect(
      resolveLegacySourceRedirect("remotemcp", "remote-1", lookup),
    ).toEqual({ kind: "mcp-server", routeParam: "acme-remote" });
  });

  it("resolves a tunneled source by id, using the server id when it has no slug", () => {
    expect(
      resolveLegacySourceRedirect("tunneledmcp", "tunnel-1", lookup),
    ).toEqual({ kind: "mcp-server", routeParam: "srv-tunnel" });
  });

  it("resolves an unproxied source by slug or id", () => {
    expect(
      resolveLegacySourceRedirect("unproxiedmcp", "vendor-mcp", lookup),
    ).toEqual({ kind: "mcp-server", routeParam: "vendor" });
  });

  it("falls back to the MCP page when the source or its server is gone", () => {
    expect(resolveLegacySourceRedirect("remotemcp", "nope", lookup)).toEqual({
      kind: "mcp-list",
    });
    expect(
      resolveLegacySourceRedirect("tunneledmcp", "tunnel-1", {
        ...lookup,
        mcpServers: [],
      }),
    ).toEqual({ kind: "mcp-list" });
  });

  it("sends an external MCP slug to the toolset carrying its tools", () => {
    expect(
      resolveLegacySourceRedirect("externalmcp", "notion", lookup),
    ).toEqual({ kind: "toolset", slug: "notion-server" });
    expect(
      resolveLegacySourceRedirect("externalmcp", "linear", lookup),
    ).toEqual({ kind: "toolset", slug: "proxy-only" });
    expect(
      resolveLegacySourceRedirect("externalmcp", "github", lookup),
    ).toEqual({ kind: "mcp-list" });
  });

  it("does not let a slug prefix match a longer slug", () => {
    expect(resolveLegacySourceRedirect("externalmcp", "notio", lookup)).toEqual(
      { kind: "mcp-list" },
    );
  });

  it("sends unknown kinds to the sources list", () => {
    expect(resolveLegacySourceRedirect(undefined, "x", lookup)).toEqual({
      kind: "sources-list",
    });
  });
});

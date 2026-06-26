import type {
  ExternalMCPRemote,
  TransportType,
} from "@gram/client/models/components";
import { describe, expect, it } from "vitest";
import { dedupeRemotesByUrl, filterToHttpRemotes } from "./remotes";
import type { PulseMCPServer } from "./hooks";

function remote(
  url: string,
  transportType: TransportType = "streamable-http",
  headerNames: string[] = [],
): ExternalMCPRemote {
  return {
    url,
    transportType,
    headers: headerNames.length
      ? headerNames.map((name) => ({
          name,
          isSecret: true,
          isRequired: true,
        }))
      : undefined,
  };
}

function server(remotes: ExternalMCPRemote[]): PulseMCPServer {
  return {
    description: "test",
    registrySpecifier: "test/server",
    version: "0.1.0",
    meta: {},
    toolCount: 0,
    isReadOnly: false,
    supportsDcr: false,
    remotes,
  };
}

describe("dedupeRemotesByUrl", () => {
  it("returns input unchanged when all URLs are unique", () => {
    const a = remote("https://a.example/mcp");
    const b = remote("https://b.example/mcp");
    expect(dedupeRemotesByUrl([a, b])).toEqual([a, b]);
  });

  it("keeps the first occurrence of each duplicate URL", () => {
    const first = remote(
      "https://mcp.datadoghq.com/v1/mcp?{TOOLSETS}",
      "streamable-http",
      [],
    );
    const second = remote(
      "https://mcp.datadoghq.com/v1/mcp?{TOOLSETS}",
      "streamable-http",
      ["DD_API_KEY", "DD_APPLICATION_KEY"],
    );
    const result = dedupeRemotesByUrl([first, second]);
    expect(result).toEqual([first]);
  });

  it("preserves order of first appearances across mixed duplicates", () => {
    const a1 = remote("https://a.example/mcp", "streamable-http", []);
    const b = remote("https://b.example/mcp");
    const a2 = remote("https://a.example/mcp", "streamable-http", [
      "Authorization",
    ]);
    expect(dedupeRemotesByUrl([a1, b, a2])).toEqual([a1, b]);
  });

  it("handles an empty list", () => {
    expect(dedupeRemotesByUrl([])).toEqual([]);
  });
});

describe("filterToHttpRemotes", () => {
  it("drops non-streamable-http remotes", () => {
    const http = remote("https://x.example/mcp", "streamable-http");
    const sse = remote("https://x.example/sse", "sse");
    const result = filterToHttpRemotes(server([http, sse]));
    expect(result.remotes).toEqual([http]);
  });

  it("collapses duplicate URLs after filtering", () => {
    const first = remote(
      "https://mcp.cloudflare.com/mcp",
      "streamable-http",
      [],
    );
    const second = remote("https://mcp.cloudflare.com/mcp", "streamable-http", [
      "Authorization",
    ]);
    const result = filterToHttpRemotes(server([first, second]));
    expect(result.remotes).toEqual([first]);
  });

  it("preserves a single streamable-http remote unchanged", () => {
    const only = remote("https://solo.example/mcp");
    const result = filterToHttpRemotes(server([only]));
    expect(result.remotes).toEqual([only]);
  });

  it("preserves all distinct streamable-http URLs (e.g. Postman regions)", () => {
    const remotes = [
      remote("https://mcp.postman.com/mcp", "streamable-http", [
        "Authorization",
      ]),
      remote("https://mcp.postman.com/minimal", "streamable-http", [
        "Authorization",
      ]),
      remote("https://mcp.eu.postman.com/mcp", "streamable-http", [
        "Authorization",
      ]),
      remote("https://mcp.eu.postman.com/minimal", "streamable-http", [
        "Authorization",
      ]),
    ];
    const result = filterToHttpRemotes(server(remotes));
    expect(result.remotes).toEqual(remotes);
  });

  it("returns undefined remotes when server has no remotes", () => {
    const result = filterToHttpRemotes(server([]));
    expect(result.remotes).toEqual([]);
  });
});

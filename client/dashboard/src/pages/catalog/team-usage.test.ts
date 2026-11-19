import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { describe, expect, it } from "vitest";
import {
  isHostableShadowServer,
  isLocalServerUrl,
  remoteMcpPrefillHref,
  teamUsedServers,
} from "./team-usage";

function server(
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

describe("isLocalServerUrl", () => {
  it.each([
    "http://localhost:3000/mcp",
    "http://127.0.0.1:8080/mcp",
    "http://[::1]:8080/mcp",
    "http://0.0.0.0/mcp",
    "http://10.1.2.3/mcp",
    "http://172.20.0.4/mcp",
    "http://192.168.1.10/mcp",
    "http://169.254.169.254/mcp",
    "http://[fd12::1]/mcp",
    "http://mcp:8080/mcp",
    "https://tools.localhost/mcp",
    "http://printer.local/mcp",
    "not a url",
  ])("treats %s as local", (url) => {
    expect(isLocalServerUrl(url)).toBe(true);
  });

  it.each([
    "https://mcp.linear.app/mcp",
    "https://8.8.8.8/mcp",
    "https://172.32.0.1/mcp",
    "https://postgres.internal.example.com/mcp",
  ])("treats %s as public", (url) => {
    expect(isLocalServerUrl(url)).toBe(false);
  });
});

describe("isHostableShadowServer", () => {
  it("keeps public HTTP servers", () => {
    expect(isHostableShadowServer(server({}))).toBe(true);
    expect(
      isHostableShadowServer(
        server({ canonicalServerUrl: "http://mcp.example.com/mcp" }),
      ),
    ).toBe(true);
  });

  it("drops local commands, local hosts and other schemes", () => {
    expect(
      isHostableShadowServer(
        server({
          targetKind: "stdio_command",
          canonicalServerUrl: "npx some-mcp",
        }),
      ),
    ).toBe(false);
    expect(
      isHostableShadowServer(
        server({ canonicalServerUrl: "http://localhost:3000/mcp" }),
      ),
    ).toBe(false);
    expect(
      isHostableShadowServer(
        server({ canonicalServerUrl: "ws://mcp.example.com/mcp" }),
      ),
    ).toBe(false);
  });
});

describe("teamUsedServers", () => {
  it("keeps hostable servers, most used first", () => {
    const sorted = teamUsedServers([
      server({
        canonicalServerUrl: "https://b.example.com/mcp",
        observedUseCount: 5,
        userCount: 1,
      }),
      server({
        canonicalServerUrl: "http://localhost:1/mcp",
        observedUseCount: 900,
      }),
      server({
        canonicalServerUrl: "https://a.example.com/mcp",
        observedUseCount: 40,
      }),
      server({
        canonicalServerUrl: "https://c.example.com/mcp",
        observedUseCount: 5,
        userCount: 3,
      }),
    ]);
    expect(sorted.map((s) => s.canonicalServerUrl)).toEqual([
      "https://a.example.com/mcp",
      "https://c.example.com/mcp",
      "https://b.example.com/mcp",
    ]);
  });
});

describe("remoteMcpPrefillHref", () => {
  it("carries the URL and the name", () => {
    const href = remoteMcpPrefillHref(
      "/org/proj/mcp/add/remote",
      server({
        serverName: "Linear",
        canonicalServerUrl: "https://mcp.linear.app/mcp",
      }),
    );
    const params = new URL(href, "https://app.test").searchParams;
    expect(params.get("url")).toBe("https://mcp.linear.app/mcp");
    expect(params.get("name")).toBe("Linear");
  });

  it("leaves the name out when the server has none", () => {
    const href = remoteMcpPrefillHref("/add", server({}));
    expect(new URL(href, "https://app.test").searchParams.has("name")).toBe(
      false,
    );
  });
});

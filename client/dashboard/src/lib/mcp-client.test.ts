import { describe, expect, it } from "vitest";
import { createMCPClientWithVersionFallback } from "./mcp-client";

const MODERN_VERSION = "2026-07-28";
const LEGACY_VERSIONS = [
  "2025-11-25",
  "2025-06-18",
  "2025-03-26",
  "2024-11-05",
];

interface FakeServerOptions {
  /** Protocol versions the server accepts in the MCP-Protocol-Version header. */
  supported: string[];
  /** Status returned for every request, overriding normal handling. */
  failStatus?: number;
}

/**
 * A minimal Streamable HTTP MCP server, implemented as a `fetch` function, that
 * records each JSON-RPC method alongside the MCP-Protocol-Version header it
 * arrived with.
 */
function fakeServer({ supported, failStatus }: FakeServerOptions) {
  const seen: string[] = [];

  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    });

  const fetch = async (
    _input: Parameters<typeof globalThis.fetch>[0],
    init?: RequestInit,
  ): Promise<Response> => {
    if (init?.method !== "POST") return new Response(null, { status: 405 });

    const headers = new Headers(init.headers);
    const version = headers.get("mcp-protocol-version");
    const msg = JSON.parse(await new Response(init.body).text()) as {
      id?: number;
      method: string;
    };
    seen.push(`${msg.method}@${version ?? "-"}`);

    if (failStatus) {
      return json(failStatus, {
        jsonrpc: "2.0",
        id: msg.id ?? null,
        error: { code: -32001, message: "Unauthorized" },
      });
    }
    if (version && !supported.includes(version)) {
      return json(400, {
        jsonrpc: "2.0",
        id: msg.id ?? null,
        error: {
          code: -32022,
          message: `Unsupported protocol version: ${version}`,
          data: { requested: version, supported },
        },
      });
    }
    if (msg.id === undefined) return new Response(null, { status: 202 });

    // The 2026-07-28 revision requires a resultType on every result.
    const reply = (result: object) =>
      json(200, {
        jsonrpc: "2.0",
        id: msg.id,
        result:
          version === MODERN_VERSION
            ? { ...result, resultType: "complete" }
            : result,
      });
    switch (msg.method) {
      case "server/discover":
        return reply({
          supportedVersions: supported,
          capabilities: { tools: {} },
        });
      case "initialize":
        return reply({
          protocolVersion: LEGACY_VERSIONS[0],
          capabilities: { tools: {} },
          serverInfo: { name: "fake", version: "1.0.0" },
        });
      case "tools/list":
        return reply({
          tools: [{ name: "run_query", inputSchema: { type: "object" } }],
        });
      default:
        return json(200, {
          jsonrpc: "2.0",
          id: msg.id,
          error: { code: -32601, message: "Method not found" },
        });
    }
  };

  return { fetch, seen };
}

async function listToolNames(server: ReturnType<typeof fakeServer>) {
  const client = await createMCPClientWithVersionFallback({
    transport: {
      type: "http",
      url: "https://mcp.example.com/mcp",
      fetch: server.fetch,
    },
  });
  try {
    return Object.keys(await client.tools());
  } finally {
    await client.close();
  }
}

describe("createMCPClientWithVersionFallback", () => {
  it("falls back to initialize when the server rejects the newest protocol version", async () => {
    const server = fakeServer({ supported: LEGACY_VERSIONS });

    expect(await listToolNames(server)).toEqual(["run_query"]);
    expect(server.seen).toEqual([
      `server/discover@${MODERN_VERSION}`,
      "initialize@2025-11-25",
      "notifications/initialized@2025-11-25",
      "tools/list@2025-11-25",
    ]);
  });

  it("keeps the newest protocol version when the server accepts it", async () => {
    const server = fakeServer({
      supported: [MODERN_VERSION, ...LEGACY_VERSIONS],
    });

    expect(await listToolNames(server)).toEqual(["run_query"]);
    expect(server.seen).toEqual([
      `server/discover@${MODERN_VERSION}`,
      `tools/list@${MODERN_VERSION}`,
    ]);
  });

  it("does not retry other connection failures", async () => {
    const server = fakeServer({ supported: LEGACY_VERSIONS, failStatus: 401 });

    await expect(listToolNames(server)).rejects.toThrow("Unauthorized");
    // Every connection attempt starts with server/discover, so a single one
    // means the helper did not retry.
    expect(
      server.seen.filter((m) => m.startsWith("server/discover")),
    ).toHaveLength(1);
  });
});

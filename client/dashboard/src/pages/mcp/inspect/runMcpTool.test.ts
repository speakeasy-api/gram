// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { runMcpTool } from "./runMcpTool";

type Call = {
  method: string;
  headers: Record<string, string>;
  body: { id?: unknown; method?: string } | undefined;
};

function jsonResponse(body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { "content-type": "application/json", ...headers },
  });
}

function stubServer(options: { sessionId?: string; failCall?: boolean }) {
  const calls: Call[] = [];
  const fetchMock = vi.fn(async (_url: string, init: RequestInit) => {
    const body =
      typeof init.body === "string"
        ? (JSON.parse(init.body) as Call["body"])
        : undefined;
    calls.push({
      method: init.method ?? "GET",
      headers: init.headers as Record<string, string>,
      body,
    });
    if (init.method === "DELETE") return new Response(null, { status: 204 });
    if (body?.method === "initialize")
      return jsonResponse(
        {
          jsonrpc: "2.0",
          id: body.id,
          result: { protocolVersion: "2025-06-18" },
        },
        options.sessionId ? { "mcp-session-id": options.sessionId } : {},
      );
    if (body?.method === "notifications/initialized")
      return new Response(null, { status: 202 });
    if (options.failCall) return new Response("nope", { status: 500 });
    return jsonResponse({
      jsonrpc: "2.0",
      id: body?.id,
      result: { content: [{ type: "text", text: "done" }] },
    });
  });
  vi.stubGlobal("fetch", fetchMock);
  return calls;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("runMcpTool", () => {
  it("initializes, notifies, calls, then closes the session", async () => {
    const calls = stubServer({ sessionId: "s-1" });
    const run = await runMcpTool({
      url: "https://mcp.example/mcp/x",
      headers: { Authorization: "Bearer t" },
      name: "search",
      args: { q: "a" },
    });

    expect(run.ok).toBe(true);
    expect(run.summary).toBe("done");
    expect(calls.map((c) => c.body?.method ?? c.method)).toEqual([
      "initialize",
      "notifications/initialized",
      "tools/call",
      "DELETE",
    ]);

    const [, notify, call, close] = calls;
    expect(notify?.body && "id" in notify.body).toBe(false);
    for (const later of [notify, call]) {
      expect(later?.headers["mcp-session-id"]).toBe("s-1");
      expect(later?.headers["MCP-Protocol-Version"]).toBe("2025-06-18");
      expect(later?.headers.Authorization).toBe("Bearer t");
    }
    expect(close?.headers["mcp-session-id"]).toBe("s-1");
  });

  it("closes the session even when the call fails", async () => {
    const calls = stubServer({ sessionId: "s-2", failCall: true });
    const run = await runMcpTool({
      url: "https://mcp.example/mcp/x",
      headers: undefined,
      name: "search",
      args: {},
    });

    expect(run.ok).toBe(false);
    expect(calls.at(-1)?.method).toBe("DELETE");
  });

  it("sends no DELETE to a stateless server", async () => {
    const calls = stubServer({});
    await runMcpTool({
      url: "https://mcp.example/mcp/x",
      headers: undefined,
      name: "search",
      args: {},
    });

    expect(calls.some((c) => c.method === "DELETE")).toBe(false);
    expect(calls[1]?.headers["mcp-session-id"]).toBeUndefined();
  });
});

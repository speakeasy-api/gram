import { afterEach, expect, it, vi } from "vitest";

import { fetchConsentInventory } from "./ConsentToolsApp";

afterEach(() => vi.unstubAllGlobals());

it.each(["tools/list", "DELETE"])(
  "honors the caller deadline while %s is stalled, including SDK cleanup",
  async (stalledMethod) => {
    const deadline = new AbortController();
    let sawStall!: () => void;
    const stalled = new Promise<void>((resolve) => {
      sawStall = resolve;
    });
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        const request =
          init?.method === "POST" ? JSON.parse(String(init.body)) : undefined;
        const method = request?.method ?? init?.method;
        if (method === stalledMethod) {
          sawStall();
          return new Promise<Response>((_resolve, reject) => {
            const abort = () => reject(init?.signal?.reason);
            if (init?.signal?.aborted) abort();
            else init?.signal?.addEventListener("abort", abort, { once: true });
          });
        }
        if (method === "GET") return new Response(null, { status: 405 });
        if (method === "DELETE") return new Response(null, { status: 200 });
        if (method === "notifications/initialized")
          return new Response(null, { status: 202 });
        const result =
          method === "initialize"
            ? {
                protocolVersion: "2025-03-26",
                capabilities: { tools: {} },
                serverInfo: { name: "test", version: "1" },
              }
            : { tools: [] };
        return new Response(
          JSON.stringify({ jsonrpc: "2.0", id: request.id, result }),
          {
            status: 200,
            headers: {
              "Content-Type": "application/json",
              "Mcp-Session-Id": "session",
            },
          },
        );
      }),
    );
    const inventory = fetchConsentInventory(
      "/consent/mcp",
      "state",
      "csrf",
      "attempt",
      deadline.signal,
    );
    // Attach rejection handling before aborting the real SDK request.
    const outcome = inventory.then(
      (value) => ({ value }),
      (error: unknown) => ({ error }),
    );
    await stalled;
    deadline.abort(new DOMException("Deadline exceeded", "TimeoutError"));
    const result = await outcome;
    if (stalledMethod === "tools/list") expect(result).toHaveProperty("error");
    else
      expect(result).toEqual({
        value: { tools: [], roleHiddenTools: { count: 0, names: [] } },
      });
  },
);

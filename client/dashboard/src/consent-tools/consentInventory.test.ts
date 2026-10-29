import { afterEach, expect, it, vi } from "vitest";

import {
  fetchConsentInventory,
  parseGatewayInventoryBootstrap,
} from "./consentInventory";

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
          init?.method === "POST" && typeof init.body === "string"
            ? JSON.parse(init.body)
            : undefined;
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
        value: {
          tools: [],
          roleHiddenTools: { count: 0, names: [] },
        },
      });
  },
);

it("accepts a well-formed gateway capture", () => {
  expect(
    parseGatewayInventoryBootstrap(
      JSON.stringify({
        attempt: "attempt",
        tools: [
          { name: "docs--search", annotations: ["read_only"], source: "Docs" },
          { name: "wiki--edit", annotations: [] },
        ],
      }),
    ),
  ).toEqual({
    attempt: "attempt",
    tools: [
      { name: "docs--search", annotations: ["read_only"], source: "Docs" },
      { name: "wiki--edit", annotations: [] },
    ],
    roleHiddenTools: { count: 0, names: [] },
  });
});

it.each([
  ["not JSON", "{"],
  ["missing attempt", JSON.stringify({ tools: [] })],
  ["non-array tools", JSON.stringify({ attempt: "a", tools: {} })],
  [
    "non-object tool",
    JSON.stringify({ attempt: "a", tools: ["docs--search"] }),
  ],
  [
    "blank name",
    JSON.stringify({ attempt: "a", tools: [{ name: "", annotations: [] }] }),
  ],
  [
    "null annotations",
    JSON.stringify({ attempt: "a", tools: [{ name: "t", annotations: null }] }),
  ],
  [
    "unknown annotation",
    JSON.stringify({
      attempt: "a",
      tools: [{ name: "t", annotations: ["sneaky"] }],
    }),
  ],
  [
    "non-string source",
    JSON.stringify({
      attempt: "a",
      tools: [{ name: "t", annotations: [], source: 7 }],
    }),
  ],
  [
    "duplicate names",
    JSON.stringify({
      attempt: "a",
      tools: [
        { name: "t", annotations: [] },
        { name: "t", annotations: [] },
      ],
    }),
  ],
])("rejects a gateway capture with %s", (_case, raw) => {
  expect(parseGatewayInventoryBootstrap(raw)).toBeNull();
});

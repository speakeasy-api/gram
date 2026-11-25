import { afterEach, expect, it, vi } from "vitest";

import {
  fetchConsentInventory,
  parseGatewayInventoryBootstrap,
} from "./consentInventory";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.useRealTimers();
});

it.each([false, true])(
  "terminates an initialized session after discovery cancellation (stalled cleanup: %s)",
  async (stallCleanup) => {
    vi.useFakeTimers();
    // Drive the timeout deterministically; native AbortSignal.timeout does not
    // use Vitest's fake timers.
    const timeout = vi
      .spyOn(AbortSignal, "timeout")
      .mockImplementation((ms) => {
        const controller = new AbortController();
        setTimeout(
          () =>
            controller.abort(
              new DOMException("Cleanup timed out", "TimeoutError"),
            ),
          ms,
        );
        return controller.signal;
      });
    const deadline = new AbortController();
    let sawDiscovery!: () => void;
    const discovery = new Promise<void>((resolve) => {
      sawDiscovery = resolve;
    });
    let sawDelete!: () => void;
    const deleting = new Promise<void>((resolve) => {
      sawDelete = resolve;
    });
    let cleanupSignal: AbortSignal | undefined;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        expect(new Headers(init?.headers).get("Gram-Consent-Auth-Client")).toBe(
          "00000000-0000-4000-8000-000000000001",
        );
        const request =
          init?.method === "POST" && typeof init.body === "string"
            ? JSON.parse(init.body)
            : undefined;
        const method = request?.method ?? init?.method;
        if (method === "tools/list" || method === "DELETE") {
          if (method === "tools/list") sawDiscovery();
          else {
            cleanupSignal = init?.signal ?? undefined;
            expect(deadline.signal.aborted).toBe(true);
            expect(cleanupSignal?.aborted).toBe(false);
            expect(new Headers(init?.headers).get("Mcp-Session-Id")).toBe(
              "session",
            );
            sawDelete();
            if (!stallCleanup) return new Response(null, { status: 200 });
          }
          return new Promise<Response>((_resolve, reject) => {
            const abort = () => reject(init?.signal?.reason);
            if (init?.signal?.aborted) abort();
            else init?.signal?.addEventListener("abort", abort, { once: true });
          });
        }
        if (method === "GET") return new Response(null, { status: 405 });
        if (method === "notifications/initialized")
          return new Response(null, { status: 202 });
        return new Response(
          JSON.stringify({
            jsonrpc: "2.0",
            id: request.id,
            result: {
              protocolVersion: "2025-03-26",
              capabilities: { tools: {} },
              serverInfo: { name: "test", version: "1" },
            },
          }),
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
      "00000000-0000-4000-8000-000000000001",
    );
    let settled = false;
    const outcome = inventory
      .then(
        (value) => ({ value }),
        (error: unknown) => ({ error }),
      )
      .finally(() => {
        settled = true;
      });
    await discovery;
    deadline.abort(new DOMException("Discovery cancelled", "AbortError"));
    await deleting;
    expect(timeout).toHaveBeenCalledExactlyOnceWith(2000);
    if (stallCleanup) {
      await vi.advanceTimersByTimeAsync(1999);
      expect(settled).toBe(false);
      expect(cleanupSignal?.aborted).toBe(false);
      await vi.advanceTimersByTimeAsync(1);
      expect(cleanupSignal?.aborted).toBe(true);
    }
    // Cleanup must not replace the original discovery error.
    expect(await outcome).toEqual({ error: deadline.signal.reason });
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

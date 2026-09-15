import type { AnalyticsQueryPayload } from "@gram/client/models/components/analyticsquerypayload.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useRunQuery } from "./useRunQuery";

interface Call {
  body: AnalyticsQueryPayload;
  signal: AbortSignal | undefined;
  resolve: (rows: unknown[]) => void;
}

const testState = vi.hoisted(() => ({
  calls: [] as Call[],
}));

vi.mock("@gram/client/funcs/analyticsQuery.js", () => ({
  analyticsQuery: (
    _client: unknown,
    request: { analyticsQueryPayload: AnalyticsQueryPayload },
    _security: unknown,
    options: { fetchOptions?: { signal?: AbortSignal } } | undefined,
  ) =>
    new Promise((resolve) => {
      testState.calls.push({
        body: request.analyticsQueryPayload,
        signal: options?.fetchOptions?.signal,
        resolve: (rows) =>
          resolve({
            ok: true,
            value: {
              dataset: request.analyticsQueryPayload.dataset,
              plan: "p",
              rows,
            },
          }),
      });
    }),
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

function body(limit: number): AnalyticsQueryPayload {
  return {
    dataset: "sessions",
    from: new Date("2026-09-14T00:00:00Z"),
    to: new Date("2026-09-15T00:00:00Z"),
    measures: [{ op: "count", alias: "count" }],
    limit,
  };
}

function wrapper({ children }: { children: ReactNode }) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useRunQuery", () => {
  beforeEach(() => {
    testState.calls = [];
  });

  it("runs nothing without a body", () => {
    const { result } = renderHook(() => useRunQuery(null), { wrapper });
    expect(result.current.fetchStatus).toBe("idle");
    expect(testState.calls).toHaveLength(0);
  });

  it("keeps the last result on screen while the next body loads", async () => {
    const { result, rerender } = renderHook(
      ({ limit }) => useRunQuery(body(limit)),
      { wrapper, initialProps: { limit: 10 } },
    );
    await waitFor(() => expect(testState.calls).toHaveLength(1));
    expect(testState.calls[0]?.signal).toBeDefined();

    act(() => {
      testState.calls[0]?.resolve([{ count: 1 }]);
    });
    await waitFor(() =>
      expect(result.current.data?.rows).toEqual([{ count: 1 }]),
    );

    rerender({ limit: 20 });
    await waitFor(() => expect(testState.calls).toHaveLength(2));
    expect(testState.calls[1]?.body.limit).toBe(20);
    expect(testState.calls[1]?.signal?.aborted).toBe(false);
    // The first result stays up, flagged as a placeholder, until the next lands.
    expect(result.current.data?.rows).toEqual([{ count: 1 }]);
    expect(result.current.isPlaceholderData).toBe(true);
    expect(result.current.isFetching).toBe(true);

    act(() => {
      testState.calls[1]?.resolve([{ count: 2 }]);
    });
    await waitFor(() =>
      expect(result.current.data?.rows).toEqual([{ count: 2 }]),
    );
    expect(result.current.isPlaceholderData).toBe(false);
  });

  it("aborts the request in flight when the body changes, rather than awaiting it", async () => {
    const { rerender } = renderHook(({ limit }) => useRunQuery(body(limit)), {
      wrapper,
      initialProps: { limit: 10 },
    });
    await waitFor(() => expect(testState.calls).toHaveLength(1));
    rerender({ limit: 20 });
    await waitFor(() => expect(testState.calls).toHaveLength(2));
    expect(testState.calls[0]?.signal?.aborted).toBe(true);
  });
});

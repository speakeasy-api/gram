import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useProxiedMcpTools } from "./useProxiedMcpTools";

const listTools = vi.hoisted(() => vi.fn());

vi.mock("@/lib/mcp-client", () => ({
  createMCPClientWithVersionFallback: async () => ({
    listTools,
    close: async () => {},
  }),
}));

function wrapper({ children }: { children: ReactNode }): JSX.Element {
  const client = new QueryClient();
  return <QueryClientProvider client={client}>{children}</QueryClientProvider>;
}

describe("useProxiedMcpTools", () => {
  beforeEach(() => listTools.mockReset());

  it("requests the whole catalog across every page and reads availability", async () => {
    listTools
      .mockResolvedValueOnce({ tools: [{ name: "one" }], nextCursor: "page-2" })
      .mockResolvedValueOnce({
        tools: [{ name: "two", _meta: { "com.speakeasy/unavailable": true } }],
      });

    const { result } = renderHook(
      () =>
        useProxiedMcpTools("https://mcp.example", {
          throwOnError: false,
          inspect: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.tools).toBeDefined());
    expect(result.current.tools?.one?.unavailable).toBe(false);
    expect(result.current.tools?.two?.unavailable).toBe(true);
    expect(listTools.mock.calls.map(([args]) => args.params)).toEqual([
      { _meta: { "com.speakeasy/inspect": true } },
      { cursor: "page-2", _meta: { "com.speakeasy/inspect": true } },
    ]);
  });

  it("keeps a known unavailable reason and drops an unknown one", async () => {
    listTools.mockResolvedValueOnce({
      tools: [
        {
          name: "blocked_tool",
          _meta: {
            "com.speakeasy/unavailable": true,
            "com.speakeasy/unavailable_reason": "blocked",
          },
        },
        {
          name: "read_only_tool",
          _meta: {
            "com.speakeasy/unavailable": true,
            "com.speakeasy/unavailable_reason": "read_only",
          },
        },
        {
          name: "future_tool",
          _meta: {
            "com.speakeasy/unavailable": true,
            "com.speakeasy/unavailable_reason": "future_reason",
          },
        },
      ],
    });

    const { result } = renderHook(
      () =>
        useProxiedMcpTools("https://mcp.example", {
          throwOnError: false,
          inspect: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.tools).toBeDefined());
    expect(result.current.tools?.blocked_tool?.unavailableReason).toBe(
      "blocked",
    );
    expect(result.current.tools?.read_only_tool?.unavailableReason).toBe(
      "read_only",
    );
    // An unknown reason must not fail the decode or hide the tool.
    expect(result.current.tools?.future_tool?.unavailable).toBe(true);
    expect(
      result.current.tools?.future_tool?.unavailableReason,
    ).toBeUndefined();
  });

  it("fails rather than following pages without end", async () => {
    let page = 0;
    listTools.mockImplementation(async () => {
      page += 1;
      return {
        tools: Array.from({ length: 1_000 }, (_, i) => ({
          name: `tool-${page}-${i}`,
        })),
        nextCursor: `page-${page}`,
      };
    });

    const { result } = renderHook(
      () =>
        useProxiedMcpTools("https://mcp.example", {
          throwOnError: false,
          inspect: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.tools).toBeUndefined();
    expect(page).toBeLessThan(20);
  });

  it("fails on empty pages with fresh cursors", async () => {
    let page = 0;
    listTools.mockImplementation(async () => {
      page += 1;
      return { tools: [], nextCursor: `page-${page}` };
    });

    const { result } = renderHook(
      () =>
        useProxiedMcpTools("https://mcp.example", {
          throwOnError: false,
          inspect: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(page).toBe(200);
  });

  it("reads only the first page, without asking to inspect, by default", async () => {
    listTools.mockResolvedValue({ tools: [{ name: "one" }], nextCursor: "b" });

    const { result } = renderHook(
      () => useProxiedMcpTools("https://mcp.example", { throwOnError: false }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.tools).toBeDefined());
    expect(listTools).toHaveBeenCalledTimes(1);
    expect(listTools).toHaveBeenCalledWith({});
  });

  it("fails rather than returning part of a catalog when a cursor repeats", async () => {
    listTools.mockResolvedValue({ tools: [{ name: "one" }], nextCursor: "a" });

    const { result } = renderHook(
      () =>
        useProxiedMcpTools("https://mcp.example", {
          throwOnError: false,
          inspect: true,
        }),
      { wrapper },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.tools).toBeUndefined();
  });
});

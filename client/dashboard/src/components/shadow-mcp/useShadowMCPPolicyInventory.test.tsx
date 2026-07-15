import { useSdkClient } from "@/contexts/Sdk";
import type { ShadowMCPInventoryServer } from "@gram/client/models/components/shadowmcpinventoryserver.js";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  initialShadowMCPPolicyURLs,
  useShadowMCPPolicyInventory,
} from "./useShadowMCPPolicyInventory";

const listInventory = vi.fn();

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: vi.fn(),
}));

function inventoryServer(
  canonicalServerUrl: string,
  overrides: Partial<ShadowMCPInventoryServer> = {},
): ShadowMCPInventoryServer {
  return {
    access: "none",
    allowedPolicyIds: [],
    canonicalServerUrl,
    firstSeen: new Date("2026-01-01T10:00:00Z"),
    lastCalled: undefined,
    lastSeen: new Date("2026-01-02T10:00:00Z"),
    observedUseCount: 0,
    requestCount: 0,
    serverName: undefined,
    serverSlug: "inventory-server-d8860eea",
    topUsers: [],
    urlHost: new URL(canonicalServerUrl).host,
    userCount: 0,
    ...overrides,
  };
}

function queryClientWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useShadowMCPPolicyInventory", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.mocked(useSdkClient).mockReturnValue({
      access: { listShadowMCPInventory: listInventory },
    } as unknown as ReturnType<typeof useSdkClient>);
  });

  it("loads every inventory page at the API maximum", async () => {
    const githubServer = inventoryServer("https://github.example.com/mcp");
    const linearServer = inventoryServer("https://linear.example.com/mcp");
    listInventory
      .mockResolvedValueOnce({
        servers: [githubServer],
        nextCursor: "page-2",
      })
      .mockResolvedValueOnce({ servers: [linearServer] });

    const { result } = renderHook(
      () => useShadowMCPPolicyInventory("project-1", true),
      { wrapper: queryClientWrapper() },
    );

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([githubServer, linearServer]);
    expect(listInventory).toHaveBeenNthCalledWith(
      1,
      {
        projectId: "project-1",
        limit: 200,
        cursor: undefined,
      },
      undefined,
      { signal: expect.any(AbortSignal) },
    );
    expect(listInventory).toHaveBeenNthCalledWith(
      2,
      {
        projectId: "project-1",
        limit: 200,
        cursor: "page-2",
      },
      undefined,
      { signal: expect.any(AbortSignal) },
    );
  });

  it("does not load inventory while disabled", async () => {
    const { result } = renderHook(
      () => useShadowMCPPolicyInventory("project-1", false),
      { wrapper: queryClientWrapper() },
    );

    expect(result.current.fetchStatus).toBe("idle");
    expect(listInventory).not.toHaveBeenCalled();
  });

  it("exposes an error and can retry loading", async () => {
    const githubServer = inventoryServer("https://github.example.com/mcp");
    listInventory
      .mockRejectedValueOnce(new Error("inventory unavailable"))
      .mockResolvedValueOnce({ servers: [githubServer] });

    const { result } = renderHook(
      () => useShadowMCPPolicyInventory("project-1", true),
      { wrapper: queryClientWrapper() },
    );

    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.error).toEqual(new Error("inventory unavailable"));

    await result.current.refetch();

    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([githubServer]);
  });
});

describe("initialShadowMCPPolicyURLs", () => {
  it("returns only URLs allowed by the edited policy", () => {
    const githubServer = inventoryServer("https://github.example.com/mcp", {
      allowedPolicyIds: ["policy-1", "policy-2"],
    });
    const linearServer = inventoryServer("https://linear.example.com/mcp", {
      allowedPolicyIds: ["policy-2"],
    });

    expect(
      initialShadowMCPPolicyURLs([githubServer, linearServer], "policy-1"),
    ).toEqual(new Set([githubServer.canonicalServerUrl]));
  });
});

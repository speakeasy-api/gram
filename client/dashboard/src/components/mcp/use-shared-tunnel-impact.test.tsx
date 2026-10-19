import type { McpServer } from "@gram/client/models/components/mcpserver.js";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useSharedTunnelImpact } from "./use-shared-tunnel-impact";

const list = vi.hoisted(() => vi.fn<() => Promise<McpServer[]>>());

// A real query behind the SDK hook, so request counts and fetch states are
// the ones TanStack Query produces.
vi.mock("@gram/client/react-query/mcpServers.js", () => ({
  useMcpServers: (
    request: { tunneledMcpServerId: string },
    _security: unknown,
    options: object,
  ) =>
    useQuery({
      queryKey: ["mcpServers", request],
      queryFn: async () => ({ mcpServers: await list() }),
      retry: false,
      ...options,
    }),
}));

const server = {
  id: "a",
  name: "Server a",
  visibility: "private",
  tunneledMcpServerId: "tunnel-1",
} as McpServer;

let queryClient: QueryClient;

// A list cached by an earlier visit, which a control must never rely on.
function cacheEarlierList() {
  queryClient.setQueryData(
    ["mcpServers", { tunneledMcpServerId: "tunnel-1" }],
    { mcpServers: [server] },
  );
}

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  queryClient = new QueryClient();
  list.mockReset();
});

afterEach(() => queryClient.clear());

describe("useSharedTunnelImpact", () => {
  it("sends no request until it is active", () => {
    renderHook(() => useSharedTunnelImpact("tunnel-1", { active: false }), {
      wrapper,
    });
    expect(list).not.toHaveBeenCalled();
  });

  it("reads the servers once when it becomes active", async () => {
    cacheEarlierList();
    list.mockResolvedValue([server]);
    const { result } = renderHook(
      () => useSharedTunnelImpact("tunnel-1", { active: true }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isReady).toBe(true));
    expect(list).toHaveBeenCalledOnce();
    expect(result.current.servers).toEqual([server]);
  });

  it("shows a retry after a failed read as loading, not as an empty list", async () => {
    cacheEarlierList();
    list.mockRejectedValue(new Error("offline"));
    const { result } = renderHook(
      () => useSharedTunnelImpact("tunnel-1", { active: true }),
      { wrapper },
    );
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.isLoading).toBe(false);

    let resolve: (servers: McpServer[]) => void = () => {};
    list.mockReturnValue(
      new Promise((done) => {
        resolve = done;
      }),
    );
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.isLoading).toBe(true));
    expect(result.current.isError).toBe(false);

    await act(async () => resolve([server]));
    await waitFor(() => expect(result.current.isReady).toBe(true));
    expect(result.current.isLoading).toBe(false);
  });
});

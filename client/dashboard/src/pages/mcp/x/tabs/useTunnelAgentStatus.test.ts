import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useTunnelAgentStatus } from "./useTunnelAgentStatus";

const mocks = vi.hoisted(() => ({
  connectionStatus: "inactive" as string,
  refetch: vi.fn<() => Promise<unknown>>(),
}));

vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => ({
    isSuccess: true,
    data: { connectionStatus: mocks.connectionStatus },
    refetch: mocks.refetch,
  }),
}));

describe("useTunnelAgentStatus", () => {
  beforeEach(() => {
    mocks.connectionStatus = "inactive";
    mocks.refetch.mockReset();
  });

  it("calls onReconnect once when the agent comes back", () => {
    const onReconnect = vi.fn<() => void>();
    const { result, rerender } = renderHook(() =>
      useTunnelAgentStatus({
        tunneledSourceId: "tun-1",
        enabled: true,
        poll: true,
        onReconnect,
      }),
    );
    expect(result.current.offline).toBe(true);
    expect(onReconnect).not.toHaveBeenCalled();

    mocks.connectionStatus = "active";
    rerender();
    expect(result.current.offline).toBe(false);
    expect(onReconnect).toHaveBeenCalledOnce();

    rerender();
    expect(onReconnect).toHaveBeenCalledOnce();
  });

  it("does not read the status of a server with no tunnel", () => {
    const { result } = renderHook(() =>
      useTunnelAgentStatus({
        tunneledSourceId: undefined,
        enabled: false,
        poll: false,
      }),
    );
    result.current.refetch();
    expect(mocks.refetch).not.toHaveBeenCalled();
  });
});

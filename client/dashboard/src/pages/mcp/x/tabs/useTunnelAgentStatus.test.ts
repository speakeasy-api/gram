import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { useTunnelAgentStatus } from "./useTunnelAgentStatus";

const mocks = vi.hoisted(() => ({
  isSuccess: true,
  connectionStatus: "inactive" as string,
  refetch: vi.fn<() => Promise<unknown>>(),
}));

vi.mock("@gram/client/react-query/getTunneledMcpServer.js", () => ({
  useGetTunneledMcpServer: () => ({
    isSuccess: mocks.isSuccess,
    data: mocks.isSuccess
      ? { connectionStatus: mocks.connectionStatus }
      : undefined,
    refetch: mocks.refetch,
  }),
}));

function renderStatus(
  initial: { tunneledSourceId?: string; enabled?: boolean } = {},
) {
  const onReconnect = vi.fn<() => void>();
  const hook = renderHook(
    ({
      tunneledSourceId,
      enabled,
    }: {
      tunneledSourceId?: string;
      enabled: boolean;
    }) =>
      useTunnelAgentStatus({
        tunneledSourceId,
        enabled,
        poll: true,
        onReconnect,
      }),
    {
      initialProps: {
        tunneledSourceId: initial.tunneledSourceId ?? "tun-1",
        enabled: initial.enabled ?? true,
      },
    },
  );
  return { ...hook, onReconnect };
}

describe("useTunnelAgentStatus", () => {
  beforeEach(() => {
    mocks.isSuccess = true;
    mocks.connectionStatus = "inactive";
    mocks.refetch.mockReset();
  });

  it("calls onReconnect once when the agent comes back", () => {
    const { result, rerender, onReconnect } = renderStatus();
    expect(result.current.offline).toBe(true);
    expect(onReconnect).not.toHaveBeenCalled();

    mocks.connectionStatus = "connected";
    rerender({ tunneledSourceId: "tun-1", enabled: true });
    expect(result.current.offline).toBe(false);
    expect(onReconnect).toHaveBeenCalledOnce();

    rerender({ tunneledSourceId: "tun-1", enabled: true });
    expect(onReconnect).toHaveBeenCalledOnce();
  });

  it("treats a failed read as unknown, not as a reconnection", () => {
    const { result, rerender, onReconnect } = renderStatus();

    mocks.isSuccess = false;
    rerender({ tunneledSourceId: "tun-1", enabled: true });
    expect(result.current.offline).toBe(false);
    expect(onReconnect).not.toHaveBeenCalled();

    mocks.isSuccess = true;
    mocks.connectionStatus = "connected";
    rerender({ tunneledSourceId: "tun-1", enabled: true });
    expect(onReconnect).toHaveBeenCalledOnce();
  });

  it("does not carry an offline read over to another source", () => {
    const { rerender, onReconnect } = renderStatus();

    mocks.connectionStatus = "connected";
    rerender({ tunneledSourceId: "tun-2", enabled: true });
    expect(onReconnect).not.toHaveBeenCalled();
  });

  it("does not call onReconnect while disabled", () => {
    const { rerender, onReconnect } = renderStatus();

    mocks.connectionStatus = "connected";
    rerender({ tunneledSourceId: "tun-1", enabled: false });
    expect(onReconnect).not.toHaveBeenCalled();

    rerender({ tunneledSourceId: "tun-1", enabled: true });
    expect(onReconnect).not.toHaveBeenCalled();
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

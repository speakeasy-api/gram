import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRemoteMcpAuthenticationProbe } from "./useRemoteMcpAuthenticationProbe";

const mocks = vi.hoisted(() => ({
  getSource: vi.fn(),
  probe: vi.fn(),
}));

let queryClient: QueryClient;

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({ remoteMcp: { probeURL: mocks.probe } }),
}));
vi.mock("@gram/client/react-query/getRemoteMcpServer.js", () => ({
  useGetRemoteMcpServer: (...args: unknown[]) => mocks.getSource(...args),
}));

function wrapper({ children }: { children: ReactNode }): JSX.Element {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, throwOnError: true } },
  });
  mocks.getSource.mockReturnValue({
    data: { url: "https://mcp.example.test" },
    isLoading: false,
    isError: false,
  });
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("useRemoteMcpAuthenticationProbe", () => {
  it("does not probe outside a resolved No Identity state", () => {
    const { result } = renderHook(
      () => useRemoteMcpAuthenticationProbe("remote-source-1", false),
      { wrapper },
    );

    expect(result.current).toBe("idle");
    expect(mocks.probe).not.toHaveBeenCalled();
  });

  it("reports the structured authentication-required outcome", async () => {
    mocks.probe.mockResolvedValue({ outcome: "authentication_required" });
    const { result, rerender } = renderHook(
      () => useRemoteMcpAuthenticationProbe("remote-source-1", true),
      { wrapper },
    );

    await waitFor(() => expect(result.current).toBe("authentication-required"));
    rerender();
    expect(mocks.probe).toHaveBeenCalledOnce();
    expect(mocks.probe).toHaveBeenCalledWith({
      probeURLForm: { url: "https://mcp.example.test" },
    });
  });

  it("does not claim authentication for an indeterminate probe", async () => {
    mocks.probe.mockResolvedValue({ outcome: "unreachable" });
    const { result } = renderHook(
      () => useRemoteMcpAuthenticationProbe("remote-source-1", true),
      { wrapper },
    );

    await waitFor(() => expect(result.current).toBe("unknown"));
  });

  it("keeps source and probe failures in the inline unknown state", async () => {
    mocks.probe.mockRejectedValue(new Error("probe failed"));
    const { result } = renderHook(
      () => useRemoteMcpAuthenticationProbe("remote-source-1", true),
      { wrapper },
    );

    await waitFor(() => expect(result.current).toBe("unknown"));
    expect(mocks.getSource).toHaveBeenCalledWith(
      { id: "remote-source-1" },
      undefined,
      { enabled: true, throwOnError: false },
    );
  });
});

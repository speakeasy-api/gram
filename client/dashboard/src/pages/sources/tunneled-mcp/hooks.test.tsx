import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  useCreateMcpServerOnExistingTunnel,
  useDeleteUnusedTunnel,
} from "./hooks";

const sdk = vi.hoisted(() => ({
  createServer: vi.fn(),
  deleteServer: vi.fn(),
  deleteTunnel: vi.fn(),
  createEndpoint: vi.fn(),
}));

vi.mock("@/contexts/Sdk", () => ({
  useSdkClient: () => ({
    mcpServers: { create: sdk.createServer, delete: sdk.deleteServer },
    tunneledMcp: { deleteServer: sdk.deleteTunnel },
    mcpEndpoints: { create: sdk.createEndpoint },
  }),
  useSlugs: () => ({ orgSlug: "acme" }),
}));
vi.mock("sonner", () => ({ toast: { warning: vi.fn(), error: vi.fn() } }));

function wrapper({ children }: { children: ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

const variables = {
  tunneledMcpServerId: "tunnel-1",
  name: "JAMF sandbox",
  userSessionIssuerId: "issuer-1",
};

beforeEach(() => {
  vi.resetAllMocks();
});

describe("useCreateMcpServerOnExistingTunnel", () => {
  it("creates a disabled server on the tunnel and its default endpoint", async () => {
    sdk.createServer.mockResolvedValue({ id: "server-1" });
    sdk.createEndpoint.mockResolvedValue({ id: "endpoint-1" });
    const { result } = renderHook(useCreateMcpServerOnExistingTunnel, {
      wrapper,
    });

    let data: unknown;
    await act(async () => {
      data = await result.current.mutateAsync(variables);
    });

    expect(sdk.createServer).toHaveBeenCalledWith({
      createMcpServerForm: {
        name: "JAMF sandbox",
        tunneledMcpServerId: "tunnel-1",
        userSessionIssuerId: "issuer-1",
        visibility: "disabled",
      },
    });
    expect(data).toEqual({
      mcpServer: { id: "server-1" },
      endpointCreated: true,
    });
  });

  it("never deletes the shared tunnel when the server create fails", async () => {
    sdk.createServer.mockRejectedValue(new Error("refused"));
    const { result } = renderHook(useCreateMcpServerOnExistingTunnel, {
      wrapper,
    });

    await act(async () => {
      await expect(result.current.mutateAsync(variables)).rejects.toThrow(
        "refused",
      );
    });

    expect(sdk.createServer).toHaveBeenCalledOnce();
    expect(sdk.deleteTunnel).not.toHaveBeenCalled();
    expect(sdk.deleteServer).not.toHaveBeenCalled();
  });

  it("keeps the server when only the endpoint fails", async () => {
    sdk.createServer.mockResolvedValue({ id: "server-1" });
    sdk.createEndpoint.mockRejectedValue(new Error("slug taken"));
    const { result } = renderHook(useCreateMcpServerOnExistingTunnel, {
      wrapper,
    });

    let data: unknown;
    await act(async () => {
      data = await result.current.mutateAsync(variables);
    });

    expect(data).toEqual({
      mcpServer: { id: "server-1" },
      endpointCreated: false,
    });
    expect(sdk.createServer).toHaveBeenCalledOnce();
    expect(sdk.deleteServer).not.toHaveBeenCalled();
    expect(sdk.deleteTunnel).not.toHaveBeenCalled();
  });
});

describe("useDeleteUnusedTunnel", () => {
  it("deletes only the tunnel, never MCP servers", async () => {
    sdk.deleteTunnel.mockResolvedValue(undefined);
    const { result } = renderHook(useDeleteUnusedTunnel, { wrapper });

    await act(async () => {
      await result.current.mutateAsync({ tunneledMcpServerId: "tunnel-1" });
    });

    expect(sdk.deleteTunnel).toHaveBeenCalledWith({ id: "tunnel-1" });
    expect(sdk.deleteServer).not.toHaveBeenCalled();
  });
});

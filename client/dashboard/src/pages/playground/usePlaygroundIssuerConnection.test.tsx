import { cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { PropsWithChildren } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import type { Toolset } from "@/lib/toolTypes";
import { usePlaygroundIssuerConnection } from "./usePlaygroundIssuerConnection";

const mocks = vi.hoisted(() => ({
  server: vi.fn(),
  mint: vi.fn(),
  probe: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({
  useProject: () => ({ id: "project", slug: "project" }),
  useSession: () => ({ user: { id: "user" }, session: "session" }),
}));
vi.mock("@/lib/utils", () => ({
  getServerURL: () => "https://platform.example",
  mcpConnectionUrl: (url: string | undefined) => url,
  firstPartyConnectUrl: (url: string | undefined) =>
    url ? `${url}/connect/first-party` : undefined,
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  useGetMcpServer: mocks.server,
}));
vi.mock("@gram/client/react-query/listDomains.js", () => ({
  useListDomains: vi.fn(),
}));
vi.mock("@gram/client/react-query/mintUserSession.js", () => ({
  useMintUserSessionMutation: () => ({ mutateAsync: mocks.mint }),
}));
vi.mock("@/hooks/useProxiedMcpTools", () => ({
  useProxiedMcpTools: mocks.probe,
}));

const toolset = {
  id: "T",
  slug: "toolset",
  mcpSlug: "canonical",
  userSessionIssuerId: "issuer-C",
} as Toolset;
const selected = {
  id: "S",
  visibility: "private",
  userSessionIssuerId: "issuer-S",
  platformEndpointSlug: "selected",
};
const clients: QueryClient[] = [];
function mount() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const wrapper = ({ children }: PropsWithChildren) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return {
    ...renderHook(() => usePlaygroundIssuerConnection(toolset), { wrapper }),
    client,
  };
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.server.mockReturnValue({ data: selected });
  mocks.mint.mockResolvedValue({ accessToken: "token-S" });
  mocks.probe.mockReturnValue({
    tools: [],
    isLoading: false,
    needsAuth: false,
    refetch: vi.fn(),
  });
  vi.spyOn(window, "open").mockReturnValue(null);
});
afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  vi.restoreAllMocks();
});

describe("usePlaygroundIssuerConnection selected target alignment", () => {
  it("mints explicitly for S and aligns probe, Connect, and the chat URL with S", async () => {
    const { result, client } = mount();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    expect(mocks.mint).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          gramProject: "project",
          mintUserSessionRequestBody: { mcpServerId: "S" },
        },
      }),
    );
    expect(
      client
        .getQueryCache()
        .findAll()
        .map((query) => query.queryKey),
    ).toContainEqual([
      "userSessionToken",
      "mcpServer",
      "project",
      "S",
      "issuer-S",
      "user",
    ]);
    expect(result.current.mcpUrl).toBe("https://platform.example/mcp/selected");
    expect(mocks.probe).toHaveBeenLastCalledWith(
      result.current.mcpUrl,
      expect.objectContaining({
        enabled: true,
        headers: { Authorization: "Bearer token-S" },
      }),
    );
    result.current.connect();
    expect(window.open).toHaveBeenCalledWith(
      "https://platform.example/mcp/selected/connect/first-party",
      "_blank",
      "noopener,noreferrer",
    );
    expect(result.current.connected).toBe(true);
  });

  it("does not gate or mint for an ungated S even when T has an issuer", () => {
    mocks.server.mockReturnValue({
      data: { ...selected, userSessionIssuerId: undefined },
    });
    const { result } = mount();
    expect(result.current.isIssuerGated).toBe(false);
    expect(result.current.accessToken).toBeUndefined();
    expect(mocks.mint).not.toHaveBeenCalled();
    expect(mocks.probe).toHaveBeenLastCalledWith(
      "https://platform.example/mcp/selected",
      expect.objectContaining({ enabled: false, headers: undefined }),
    );
  });

  it.each(["disabled", "loading", "error", "missingEndpoint", "customOnly"])(
    "does not mint, probe or connect for %s",
    (state) => {
      if (state === "disabled")
        mocks.server.mockReturnValue({
          data: { ...selected, visibility: "disabled" },
        });
      if (state === "loading")
        mocks.server.mockReturnValue({ isLoading: true });
      if (state === "error")
        mocks.server.mockReturnValue({
          isError: true,
          error: new Error("lookup failed"),
        });
      // Both missing and custom-only endpoints omit the platform address in the API.
      if (state === "missingEndpoint" || state === "customOnly")
        mocks.server.mockReturnValue({
          data: { ...selected, platformEndpointSlug: undefined },
        });
      const { result } = mount();
      expect(result.current.mcpUrl).toBeUndefined();
      expect(result.current.canConnect).toBe(false);
      expect(mocks.mint).not.toHaveBeenCalled();
      expect(mocks.probe).toHaveBeenLastCalledWith(
        undefined,
        expect.objectContaining({ enabled: false }),
      );
      result.current.connect();
      expect(window.open).not.toHaveBeenCalled();
    },
  );
  it("keeps legacy mint and route together only when no wrapper exists", async () => {
    const error = new ServiceError(
      {
        fault: false,
        id: "error",
        message: "not found",
        name: "not_found",
        temporary: false,
        timeout: false,
      },
      {
        request: new Request("https://platform.example"),
        response: new Response(null, { status: 404 }),
        body: "",
      },
    );
    mocks.server.mockReturnValue({ isError: true, error });
    const { result, client } = mount();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    expect(mocks.mint).toHaveBeenCalledWith(
      expect.objectContaining({
        request: {
          gramProject: "project",
          mintUserSessionRequestBody: { toolsetId: "T" },
        },
      }),
    );
    expect(
      client
        .getQueryCache()
        .findAll()
        .map((query) => query.queryKey),
    ).toContainEqual([
      "userSessionToken",
      "toolset",
      "project",
      "T",
      "issuer-C",
      "user",
    ]);
    expect(result.current.mcpUrl).toBe(
      "https://platform.example/mcp/canonical",
    );
  });
  it("changes mint identity and issuer when authoritative selection changes", async () => {
    const { result, rerender, client } = mount();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    mocks.server.mockReturnValue({
      data: {
        ...selected,
        id: "S2",
        userSessionIssuerId: "issuer-S2",
        platformEndpointSlug: "selected-2",
      },
    });
    mocks.mint.mockResolvedValue({ accessToken: "token-S2" });
    rerender();
    expect(result.current.accessToken).toBeUndefined();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S2"));
    expect(mocks.mint).toHaveBeenLastCalledWith(
      expect.objectContaining({
        request: {
          gramProject: "project",
          mintUserSessionRequestBody: { mcpServerId: "S2" },
        },
      }),
    );
    expect(
      client
        .getQueryCache()
        .findAll()
        .map((query) => query.queryKey),
    ).toContainEqual([
      "userSessionToken",
      "mcpServer",
      "project",
      "S2",
      "issuer-S2",
      "user",
    ]);
    expect(mocks.probe).toHaveBeenLastCalledWith(
      "https://platform.example/mcp/selected-2",
      expect.objectContaining({
        headers: { Authorization: "Bearer token-S2" },
      }),
    );
    expect(result.current.mcpUrl).toBe(
      "https://platform.example/mcp/selected-2",
    );
    result.current.connect();
    expect(window.open).toHaveBeenLastCalledWith(
      "https://platform.example/mcp/selected-2/connect/first-party",
      "_blank",
      "noopener,noreferrer",
    );
  });
});

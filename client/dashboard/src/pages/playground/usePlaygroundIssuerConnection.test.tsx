import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import {
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
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
    defaultOptions: { queries: { retry: false, throwOnError: true } },
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
    isError: false,
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
  it("blocks non-401 probe failures even with cached tools", async () => {
    mocks.probe.mockReturnValue({
      tools: [],
      isLoading: false,
      isError: true,
      needsAuth: false,
      error: new Error("HTTP 503 Service Unavailable"),
      refetch: vi.fn(),
    });
    const { result } = mount();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    expect(result.current.isError).toBe(true);
    expect(result.current.errorMessage).toContain("Unable to connect");
    expect(result.current.connected).toBe(false);
    expect(result.current.needsAuth).toBe(false);
  });

  it("keeps a 401 as a needs-auth state, not a connection error", async () => {
    mocks.probe.mockReturnValue({
      tools: undefined,
      isLoading: false,
      isError: true,
      needsAuth: true,
      error: new Error("HTTP 401 Unauthorized"),
      refetch: vi.fn(),
    });
    const { result } = mount();
    await waitFor(() => expect(result.current.accessToken).toBe("token-S"));
    expect(result.current.needsAuth).toBe(true);
    expect(result.current.isError).toBe(false);
    expect(result.current.errorMessage).toBeUndefined();
    expect(result.current.connected).toBe(false);
  });

  it("surfaces mint failure inline without probing", async () => {
    mocks.mint.mockRejectedValue(new Error("mint failed"));
    const { result } = mount();
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.errorMessage).toContain("Unable to create a session");
    expect(result.current.isLoading).toBe(false);
    expect(result.current.connected).toBe(false);
    expect(result.current.accessToken).toBeUndefined();
    expect(mocks.probe).toHaveBeenLastCalledWith(
      "https://platform.example/mcp/selected",
      expect.objectContaining({ enabled: false }),
    );
  });

  it.each([
    ["loading", undefined],
    ["error", "Unable to load"],
    ["disabled", "disabled"],
    ["noAddress", "no platform address"],
  ])("distinguishes the %s target state", (state, message) => {
    mocks.server.mockReturnValue(
      state === "loading"
        ? { isLoading: true }
        : state === "error"
          ? { isError: true, error: new Error("lookup failed") }
          : {
              data: {
                ...selected,
                ...(state === "disabled"
                  ? { visibility: "disabled" }
                  : { platformEndpointSlug: undefined }),
              },
            },
    );
    const { result } = mount();
    expect(result.current.isLoading).toBe(state === "loading");
    expect(result.current.isError).toBe(state !== "loading");
    if (message) expect(result.current.errorMessage).toContain(message);
    else expect(result.current.errorMessage).toBeUndefined();
  });

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

// Exercise query transitions, not only static status mocks: the hooks keep
// cached data after refresh errors and refetch must not restart earlier stages.
function liveQueries() {
  const lookup = vi.fn().mockResolvedValue(selected);
  const probe = vi.fn().mockResolvedValue({});
  mocks.server.mockImplementation(function useServer() {
    return useQuery({
      queryKey: ["selected-server"],
      queryFn: lookup,
      retry: false,
      throwOnError: false,
    });
  });
  mocks.probe.mockImplementation(function useProbe(
    url: string | undefined,
    options: { enabled: boolean; headers?: { Authorization: string } },
  ) {
    const query = useQuery({
      queryKey: [
        "proxiedMcpTools",
        url,
        options.headers
          ? [`Authorization:${options.headers.Authorization}`]
          : [],
      ],
      queryFn: probe,
      enabled: options.enabled,
      retry: false,
      throwOnError: false,
      staleTime: Infinity,
    });
    return {
      tools: query.data,
      isLoading: query.isLoading,
      isError: query.isError,
      needsAuth: query.isError && query.error?.message.includes("401"),
      refetch: () => void query.refetch(),
    };
  });
  return { lookup, probe };
}
describe("stage-aware retries", () => {
  it.each(["lookup", "mint", "probe", "401"])(
    "recovers a transient %s failure without retrying earlier stages",
    async (stage) => {
      const { lookup, probe } = liveQueries();
      const failed =
        stage === "lookup" ? lookup : stage === "mint" ? mocks.mint : probe;
      failed.mockRejectedValueOnce(
        new Error(stage === "401" ? "HTTP 401" : "temporary failure"),
      );
      const { result } = mount();
      await waitFor(() =>
        expect(
          stage === "401" ? result.current.needsAuth : result.current.isError,
        ).toBe(true),
      );
      if (stage === "401") {
        result.current.connect();
        expect(window.open).toHaveBeenCalledWith(
          "https://platform.example/mcp/selected/connect/first-party",
          "_blank",
          "noopener,noreferrer",
        );
      }
      let finish!: (value: unknown) => void;
      failed.mockImplementationOnce(
        () =>
          new Promise((resolve) => {
            finish = resolve;
          }),
      );
      act(() => {
        result.current.refetch();
        result.current.refetch();
      });
      await waitFor(() => expect(result.current.isLoading).toBe(true));
      expect(result.current.connected).toBe(false);
      await act(async () =>
        finish(
          stage === "lookup"
            ? selected
            : stage === "mint"
              ? { accessToken: "token-S" }
              : {},
        ),
      );
      await waitFor(() => expect(result.current.connected).toBe(true));
      expect(lookup).toHaveBeenCalledTimes(stage === "lookup" ? 2 : 1);
      expect(mocks.mint).toHaveBeenCalledTimes(stage === "mint" ? 2 : 1);
      expect(probe).toHaveBeenCalledTimes(
        stage === "probe" || stage === "401" ? 2 : 1,
      );
      for (const [request] of mocks.mint.mock.calls) {
        expect(request.request.mintUserSessionRequestBody).toEqual({
          mcpServerId: "S",
        });
      }
    },
  );
  it.each(["disabled", "addressless"])(
    "refreshes the authoritative %s target before minting",
    async (state) => {
      const { lookup } = liveQueries();
      lookup.mockResolvedValueOnce({
        ...selected,
        ...(state === "disabled"
          ? { visibility: "disabled" }
          : { platformEndpointSlug: undefined }),
      });
      const { result } = mount();
      await waitFor(() => expect(result.current.isError).toBe(true));
      expect(mocks.mint).not.toHaveBeenCalled();
      act(() => result.current.refetch());
      await waitFor(() => expect(result.current.connected).toBe(true));
      expect(lookup).toHaveBeenCalledTimes(2);
      expect(mocks.mint).toHaveBeenCalledTimes(1);
    },
  );
  it("does not use a cached token after a failed refresh", async () => {
    const { probe } = liveQueries();
    const { result, client } = mount();
    await waitFor(() => expect(result.current.connected).toBe(true));
    mocks.mint.mockRejectedValueOnce(new Error("refresh failed"));
    await act(async () => {
      await client.refetchQueries({ queryKey: ["userSessionToken"] });
    });
    await waitFor(() => expect(result.current.isError).toBe(true));
    expect(result.current.accessToken).toBe("token-S");
    expect(result.current.connected).toBe(false);
    const probes = probe.mock.calls.length;
    act(() => result.current.refetch());
    await waitFor(() => expect(result.current.connected).toBe(true));
    expect(probe).toHaveBeenCalledTimes(probes);
    expect(mocks.mint).toHaveBeenCalledTimes(3);
  });
});

it("re-resolves a failed lookup before minting for a changed selection", async () => {
  const { lookup } = liveQueries();
  const { result, client } = mount();
  await waitFor(() => expect(result.current.connected).toBe(true));
  lookup.mockRejectedValueOnce(new Error("lookup refresh failed"));
  await act(async () => {
    await client.refetchQueries({ queryKey: ["selected-server"] });
  });
  await waitFor(() => expect(result.current.isError).toBe(true));
  expect(result.current.accessToken).toBeUndefined();
  lookup.mockResolvedValueOnce({
    ...selected,
    id: "S2",
    platformEndpointSlug: "selected-2",
    userSessionIssuerId: "issuer-S2",
  });
  mocks.mint.mockResolvedValueOnce({ accessToken: "token-S2" });
  act(() => result.current.refetch());
  await waitFor(() => expect(result.current.connected).toBe(true));
  expect(mocks.mint).toHaveBeenCalledTimes(2);
  expect(
    mocks.mint.mock.calls.map(
      ([request]) => request.request.mintUserSessionRequestBody,
    ),
  ).toEqual([{ mcpServerId: "S" }, { mcpServerId: "S2" }]);
  expect(result.current.accessToken).toBe("token-S2");
  expect(result.current.mcpUrl).toBe("https://platform.example/mcp/selected-2");
});

it.each(["userSessionToken", "proxiedMcpTools"])(
  "keeps healthy cached chat connected during %s refresh",
  async (key) => {
    const { probe } = liveQueries();
    const { result, client } = mount();
    await waitFor(() => expect(result.current.connected).toBe(true));
    const request = key === "userSessionToken" ? mocks.mint : probe;
    let finish!: (value: unknown) => void;
    request.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    act(() => {
      void client.refetchQueries({ queryKey: [key] });
    });
    await waitFor(() => expect(client.isFetching({ queryKey: [key] })).toBe(1));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.connected).toBe(true);
    expect(result.current.accessToken).toBe("token-S");
    expect(result.current.isError).toBe(false);
    // Even though this healthy refresh does not block chat, Retry cannot cancel
    // it or start a second request against the same target.
    const requests = request.mock.calls.length;
    act(() => result.current.refetch());
    expect(request).toHaveBeenCalledTimes(requests);
    await act(async () =>
      finish(key === "userSessionToken" ? { accessToken: "token-S" } : {}),
    );
    await waitFor(() => expect(client.isFetching({ queryKey: [key] })).toBe(0));
    expect(result.current.connected).toBe(true);
  },
);

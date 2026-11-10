import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ServiceError } from "@gram/client/models/errors/serviceerror.js";
import { useToolsetMcpTarget } from "./useToolsetUrl";

const mocks = vi.hoisted(() => ({ server: vi.fn(), lookup: vi.fn() }));
vi.mock("@/contexts/Auth", () => ({ useProject: () => ({ slug: "project" }) }));
vi.mock("@/lib/utils", () => ({
  getServerURL: () => "https://platform.example",
}));
vi.mock("@gram/client/react-query/getMcpServer.js", () => ({
  buildGetMcpServerQuery: mocks.lookup,
}));
vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));
vi.mock("@tanstack/react-query", () => ({ useQuery: mocks.server }));
vi.mock("@gram/client/react-query/listDomains.js", () => ({
  useListDomains: vi.fn(),
}));

const toolset = {
  id: "T",
  slug: "toolset",
  mcpSlug: "canonical",
  defaultEnvironmentSlug: "default",
  userSessionIssuerId: "issuer-C",
};
const selected = {
  id: "S",
  visibility: "private",
  userSessionIssuerId: "issuer-S",
  platformEndpointSlug: "selected",
};
function serviceError(status: number) {
  return new ServiceError(
    {
      fault: false,
      id: "error",
      message: "lookup failed",
      name: "lookup",
      temporary: false,
      timeout: false,
    },
    {
      request: new Request("https://platform.example"),
      response: new Response(null, { status }),
      body: "",
    },
  );
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.lookup.mockReturnValue({ queryKey: ["server"], queryFn: vi.fn() });
  mocks.server.mockReturnValue({
    data: selected,
    isLoading: false,
    isError: false,
  });
});
afterEach(cleanup);

describe("useToolsetMcpTarget", () => {
  it("returns identity, endpoint and issuer from one authoritative lookup", () => {
    const { result } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(mocks.lookup).toHaveBeenCalledWith({}, { toolsetId: "T" });
    expect(mocks.server).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        queryKey: ["server", "connectionTarget"],
      }),
    );
    expect(result.current).toMatchObject({
      url: "https://platform.example/mcp/selected",
      serverId: "S",
      userSessionIssuerId: "issuer-S",
      legacy: false,
    });
  });
  it("does not inherit the toolset issuer for ungated S", () => {
    mocks.server.mockReturnValue({
      data: { ...selected, userSessionIssuerId: undefined },
    });
    expect(
      renderHook(() => useToolsetMcpTarget(toolset)).result.current
        .userSessionIssuerId,
    ).toBeUndefined();
  });
  it.each([
    "disabled",
    "loading",
    "forbidden",
    "network",
    "untyped404",
    "serverError",
    "missingEndpoint",
    "customOnly",
  ])("fails closed for %s", (state) => {
    if (state === "disabled")
      mocks.server.mockReturnValue({
        data: { ...selected, visibility: "disabled" },
      });
    if (state === "loading") mocks.server.mockReturnValue({ isLoading: true });
    if (state === "forbidden")
      mocks.server.mockReturnValue({ isError: true, error: serviceError(403) });
    if (state === "network")
      mocks.server.mockReturnValue({
        isError: true,
        error: new Error("network"),
      });
    if (state === "untyped404")
      mocks.server.mockReturnValue({
        isError: true,
        error: { statusCode: 404 },
      });
    if (state === "serverError")
      mocks.server.mockReturnValue({ isError: true, error: serviceError(500) });
    // Both missing and custom-only endpoints omit the platform address in the API.
    if (state === "missingEndpoint" || state === "customOnly")
      mocks.server.mockReturnValue({
        data: { ...selected, platformEndpointSlug: undefined },
      });
    const { result } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(result.current.url).toBeUndefined();
    expect(result.current.legacy).toBe(false);
    const expectedStatus =
      state === "disabled"
        ? "unavailable"
        : state === "loading"
          ? "loading"
          : state === "missingEndpoint" || state === "customOnly"
            ? "ready"
            : "error";
    expect(result.current.status).toBe(expectedStatus);
    if (state === "loading") expect(result.current.isLoading).toBe(true);
  });
  it("allows legacy routing for the normalized no-wrapper result", () => {
    mocks.server.mockReturnValue({ data: null });
    expect(
      renderHook(() => useToolsetMcpTarget(toolset)).result.current,
    ).toMatchObject({
      url: "https://platform.example/mcp/canonical",
      userSessionIssuerId: "issuer-C",
      legacy: true,
      status: "ready",
    });
  });
  it.each([403, 404])(
    "discards stale selected identity after a %s refetch",
    (status) => {
      const { result, rerender } = renderHook(() =>
        useToolsetMcpTarget(toolset),
      );
      mocks.server.mockReturnValue(
        status === 404
          ? { data: null }
          : {
              data: selected,
              isError: true,
              error: serviceError(status),
            },
      );
      rerender();
      expect(result.current.serverId).toBeUndefined();
      expect(result.current.userSessionIssuerId).toBe(
        status === 404 ? "issuer-C" : undefined,
      );
      expect(result.current.url).toBe(
        status === 404 ? "https://platform.example/mcp/canonical" : undefined,
      );
    },
  );
  it("has no resolved identity before selecting a toolset", () => {
    const { result } = renderHook(() => useToolsetMcpTarget(undefined));
    expect(result.current.status).toBe("idle");
    expect(result.current.userSessionIssuerId).toBeUndefined();
  });
  it("confirms an ungated selected identity without inheriting the toolset issuer", () => {
    mocks.server.mockReturnValue({
      data: { ...selected, userSessionIssuerId: undefined },
    });
    const { result } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(result.current.status).toBe("ready");
    expect(result.current.userSessionIssuerId).toBeUndefined();
  });
  it("retries lookup without reviving retained identity and then accepts the new target", async () => {
    const refetch = vi.fn().mockResolvedValue({});
    mocks.server.mockReturnValue({
      data: selected,
      isError: true,
      error: serviceError(503),
      refetch,
    });
    const { result, rerender } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(result.current.status).toBe("error");
    act(() => result.current.refetch());
    expect(refetch).toHaveBeenCalledOnce();
    expect(refetch).toHaveBeenCalledWith({
      throwOnError: false,
      cancelRefetch: false,
    });
    mocks.server.mockReturnValue({
      data: selected,
      isError: true,
      isFetching: true,
      error: serviceError(503),
      refetch,
    });
    rerender();
    expect(result.current.status).toBe("loading");
    expect(result.current.serverId).toBeUndefined();
    expect(result.current.url).toBeUndefined();
    mocks.server.mockReturnValue({
      data: {
        ...selected,
        id: "S2",
        userSessionIssuerId: "issuer-S2",
        platformEndpointSlug: "selected-2",
      },
      refetch,
    });
    await act(async () => rerender());
    expect(result.current).toMatchObject({
      status: "ready",
      serverId: "S2",
      userSessionIssuerId: "issuer-S2",
      url: "https://platform.example/mcp/selected-2",
    });
  });
  it.each([
    { ...selected, visibility: "disabled" },
    { ...selected, platformEndpointSlug: undefined },
  ])("preserves settled unusable targets during background refresh", (data) => {
    mocks.server.mockReturnValue({ data, isFetching: true });
    const { result } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(result.current.status).toBe(
      data.visibility === "disabled" ? "unavailable" : "ready",
    );
  });
  it("does not carry a settled legacy fallback into another selection", () => {
    mocks.server.mockReturnValue({ data: null });
    const { result, rerender } = renderHook(
      ({ id }) => useToolsetMcpTarget({ ...toolset, id }),
      { initialProps: { id: "T" } },
    );
    expect(result.current.legacy).toBe(true);
    mocks.server.mockReturnValue({
      isPending: true,
      isLoading: true,
      isFetching: true,
    });
    rerender({ id: "T2" });
    expect(result.current).toMatchObject({ legacy: false, status: "loading" });
    expect(result.current.url).toBeUndefined();
    expect(result.current.userSessionIssuerId).toBeUndefined();
  });
  it("keeps healthy cached targets ready during background refresh", () => {
    mocks.server.mockReturnValue({ data: selected, isFetching: true });
    const { result } = renderHook(() => useToolsetMcpTarget(toolset));
    expect(result.current).toMatchObject({
      status: "ready",
      serverId: selected.id,
    });
  });
  it("does not refetch an unselected target", () => {
    const refetch = vi.fn();
    mocks.server.mockReturnValue({ refetch });
    const { result } = renderHook(() => useToolsetMcpTarget(undefined));
    act(() => result.current.refetch());
    expect(refetch).not.toHaveBeenCalled();
  });
  it("updates the complete selected tuple together", () => {
    const { result, rerender } = renderHook(() => useToolsetMcpTarget(toolset));
    mocks.server.mockReturnValue({
      data: {
        ...selected,
        id: "S2",
        userSessionIssuerId: "issuer-S2",
        platformEndpointSlug: "selected-2",
      },
    });
    rerender();
    expect(result.current).toMatchObject({
      serverId: "S2",
      userSessionIssuerId: "issuer-S2",
      url: "https://platform.example/mcp/selected-2",
    });
  });
});

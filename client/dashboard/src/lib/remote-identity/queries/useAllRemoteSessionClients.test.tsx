import { cleanup, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAllRemoteSessionClients } from "./useAllRemoteSessionClients";

const mocks = vi.hoisted(() => ({ query: vi.fn(), fetchNextPage: vi.fn() }));

vi.mock("@gram/client/react-query/remoteSessionClients.js", () => ({
  useRemoteSessionClientsInfinite: () => mocks.query(),
}));

const first = { id: "client-1" };
const second = { id: "client-2" };
const page = (...items: unknown[]) => ({ result: { items } });

function renderClients(query: Record<string, unknown>) {
  mocks.query.mockReturnValue({
    hasNextPage: false,
    isFetchingNextPage: false,
    fetchNextPage: mocks.fetchNextPage,
    ...query,
  });
  return renderHook(() => useAllRemoteSessionClients({})).result.current;
}

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("useAllRemoteSessionClients", () => {
  it("is loading, not refreshing, on the first page", () => {
    expect(renderClients({ isFetching: true })).toMatchObject({
      items: [],
      isLoading: true,
      isRefreshing: false,
    });
  });

  it("walks later pages without calling that a refresh", () => {
    const clients = renderClients({
      isFetching: false,
      hasNextPage: true,
      data: { pages: [page(first)] },
    });
    expect(clients).toMatchObject({ isLoading: true, isRefreshing: false });
    expect(mocks.fetchNextPage).toHaveBeenCalledOnce();
  });

  it("flattens every page once the list has loaded", () => {
    expect(
      renderClients({
        isFetching: false,
        data: { pages: [page(first), page(second)] },
      }),
    ).toMatchObject({
      items: [first, second],
      isLoading: false,
      isRefreshing: false,
    });
  });

  it("reports a refetch of a loaded list as a refresh", () => {
    expect(
      renderClients({ isFetching: true, data: { pages: [page(first)] } }),
    ).toMatchObject({
      items: [first],
      isLoading: true,
      isRefreshing: true,
    });
  });
});

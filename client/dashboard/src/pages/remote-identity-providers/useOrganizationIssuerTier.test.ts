import { renderHook } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useOrganizationIssuerTier } from "./useOrganizationIssuerTier";

const mocks = vi.hoisted(() => ({
  query: vi.fn(),
  fetchNextPage: vi.fn(),
}));

vi.mock("@gram/client/react-query/organizationRemoteSessionIssuers.js", () => ({
  useOrganizationRemoteSessionIssuersInfinite: () => mocks.query(),
}));

afterEach(() => {
  vi.clearAllMocks();
});

function page(id: string) {
  return { result: { items: [{ issuer: { id }, clientCount: 0 }] } };
}

it("walks every page of a drained tier", () => {
  mocks.query.mockReturnValue({
    data: { pages: [page("a")] },
    isLoading: false,
    isError: false,
    hasNextPage: true,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    fetchNextPage: mocks.fetchNextPage,
  });

  const { result } = renderHook(() =>
    useOrganizationIssuerTier("organization", { drain: true }),
  );

  expect(mocks.fetchNextPage).toHaveBeenCalledOnce();
  // Not settled until the last page lands.
  expect(result.current.isLoading).toBe(true);
});

it("stops draining at a failed page and reports it", () => {
  mocks.query.mockReturnValue({
    data: { pages: [page("a")] },
    isLoading: false,
    isError: true,
    // The last good page still reports a next one.
    hasNextPage: true,
    isFetchingNextPage: false,
    isFetchNextPageError: true,
    fetchNextPage: mocks.fetchNextPage,
  });

  const { result } = renderHook(() =>
    useOrganizationIssuerTier("organization", { drain: true }),
  );

  expect(mocks.fetchNextPage).not.toHaveBeenCalled();
  expect(result.current.isLoading).toBe(false);
  expect(result.current.isError).toBe(true);
  expect(result.current.items).toHaveLength(1);
});

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

it("walks a drained tier to its last page, then settles", () => {
  // Mirrors react-query: a fetch goes in flight, then its page lands. The
  // third page is the last.
  const pages = [page("a")];
  let inFlight = false;
  mocks.fetchNextPage.mockImplementation(() => {
    inFlight = true;
  });
  mocks.query.mockImplementation(() => ({
    data: { pages: [...pages] },
    isLoading: false,
    isError: false,
    hasNextPage: pages.length < 3,
    isFetchingNextPage: inFlight,
    isFetchNextPageError: false,
    fetchNextPage: mocks.fetchNextPage,
  }));

  const { result, rerender } = renderHook(() =>
    useOrganizationIssuerTier("organization", { drain: true }),
  );
  while (inFlight || result.current.isLoading) {
    expect(mocks.fetchNextPage.mock.calls.length).toBeLessThan(5);
    rerender(); // the fetch is in flight
    pages.push(page(`p${pages.length}`));
    inFlight = false;
    rerender(); // its page has landed
  }

  expect(mocks.fetchNextPage).toHaveBeenCalledTimes(2);
  expect(result.current.items).toHaveLength(3);
  expect(result.current.hasMore).toBe(false);
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

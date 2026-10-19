import { renderHook } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import {
  retryFailedTiers,
  useIssuerTier,
  type IssuerTier,
} from "./useIssuerTier";

const query = vi.hoisted(() => ({
  state: {
    data: undefined,
    isLoading: false,
    isError: false,
    isFetching: false,
    hasNextPage: true,
    isFetchNextPageError: false,
    fetchNextPage: vi.fn(),
    refetch: vi.fn(),
  },
}));

vi.mock("@gram/client/react-query/organizationUserSessionIssuers.js", () => ({
  useOrganizationUserSessionIssuersInfinite: () => query.state,
}));

beforeEach(() => {
  query.state.isLoading = false;
  query.state.isError = false;
  query.state.isFetching = false;
  query.state.hasNextPage = true;
  query.state.isFetchNextPageError = false;
  query.state.fetchNextPage.mockReset();
});

describe("useIssuerTier", () => {
  it("loads the next page only once no fetch is in progress", () => {
    query.state.isFetching = true;
    const { rerender } = renderHook(() => useIssuerTier("organization"));
    expect(query.state.fetchNextPage).not.toHaveBeenCalled();

    query.state.isFetching = false;
    rerender();
    expect(query.state.fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("stops loading pages after one fails", () => {
    query.state.isFetchNextPageError = true;
    renderHook(() => useIssuerTier("organization"));
    expect(query.state.fetchNextPage).not.toHaveBeenCalled();
  });

  it("reports a background refresh as fetching", () => {
    query.state.hasNextPage = false;
    query.state.isFetching = true;
    const { result } = renderHook(() => useIssuerTier("organization"));
    expect(result.current.isLoading).toBe(false);
    expect(result.current.isFetching).toBe(true);
  });

  it("reports a retry after a failure as loading", () => {
    query.state.hasNextPage = false;
    query.state.isError = true;
    query.state.isFetching = true;
    const { result } = renderHook(() => useIssuerTier("organization"));
    expect(result.current.isLoading).toBe(true);
  });
});

describe("retryFailedTiers", () => {
  function tier(isError: boolean): { tier: IssuerTier; retried: () => number } {
    let count = 0;
    return {
      tier: {
        items: [],
        isLoading: false,
        isError,
        isFetching: false,
        refresh: () => Promise.resolve(),
        retry: () => {
          count++;
        },
      },
      retried: () => count,
    };
  }

  it("retries only the tiers that failed", () => {
    const healthy = tier(false);
    const failed = tier(true);

    retryFailedTiers([healthy.tier, failed.tier])?.();

    expect(failed.retried()).toBe(1);
    expect(healthy.retried()).toBe(0);
  });

  it("offers nothing to retry when no tier failed", () => {
    expect(
      retryFailedTiers([tier(false).tier, tier(false).tier]),
    ).toBeUndefined();
  });
});

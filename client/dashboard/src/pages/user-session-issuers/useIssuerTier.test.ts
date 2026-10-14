import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
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

describe("useIssuerTier", () => {
  it("loads the next page only once no fetch is in progress", () => {
    query.state.isFetching = true;
    const { rerender } = renderHook(() => useIssuerTier("organization"));
    expect(query.state.fetchNextPage).not.toHaveBeenCalled();

    query.state.isFetching = false;
    rerender();
    expect(query.state.fetchNextPage).toHaveBeenCalledTimes(1);
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

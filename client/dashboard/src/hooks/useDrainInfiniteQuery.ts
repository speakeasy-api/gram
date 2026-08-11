import { useEffect } from "react";

interface DrainableInfiniteQuery {
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  isError: boolean;
  fetchNextPage: () => Promise<unknown>;
}

/**
 * Fetches every remaining page of an infinite query so state derived from it
 * (membership sets, pickers, counts) reflects the complete collection instead
 * of just the loaded pages. Stops on error so a failing endpoint is not
 * hammered in a loop.
 */
export function useDrainInfiniteQuery(
  query: DrainableInfiniteQuery,
  enabled = true,
): void {
  const { hasNextPage, isFetchingNextPage, isError, fetchNextPage } = query;
  useEffect(() => {
    if (!enabled || !hasNextPage || isFetchingNextPage || isError) return;
    void fetchNextPage();
  }, [enabled, hasNextPage, isFetchingNextPage, isError, fetchNextPage]);
}

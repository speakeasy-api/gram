import { useEffect } from "react";

/** Rows fetched per request. The list endpoints cap `limit` at 100. */
export const PAGE_FETCH_LIMIT = 100;

/**
 * Stop after this many pages. The Clients and Sessions tab counts, searches,
 * sorts, and paginates over the whole set in memory, so it has to pull every
 * cursor page up front. The cap bounds that for an issuer with an unusually
 * large number of sessions: better to show a truncation notice than to fire
 * hundreds of requests on tab open.
 */
export const MAX_PAGES = 10;

/**
 * Drives a cursor-paginated infinite query to completion, one page at a time.
 *
 * Returns whether it stopped short of the end, which the caller must surface —
 * otherwise a partial set reads as the complete one, and every count and
 * "N of M" on the page quietly understates.
 */
export function useDrainedPages({
  enabled = true,
  pageCount,
  hasNextPage,
  isFetchingNextPage,
  isFetchNextPageError,
  fetchNextPage,
}: {
  /** Set false while the underlying query is itself disabled. */
  enabled?: boolean;
  pageCount: number;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  /**
   * Whether the last page fetch failed. Load-bearing: a failed fetch leaves
   * `hasNextPage` true and `pageCount` unchanged, so without this the effect's
   * dependencies settle back to exactly the state that triggered it and it
   * re-fires immediately, in a tight loop with no backoff.
   */
  isFetchNextPageError: boolean;
  /**
   * Returns a promise the caller ignores; typed loosely so react-query's
   * value-returning fetchNextPage can be passed straight through.
   */
  fetchNextPage: () => unknown;
}): { isTruncated: boolean } {
  const atCap = pageCount >= MAX_PAGES;
  const stopped = !enabled || atCap || isFetchNextPageError;

  useEffect(() => {
    if (stopped || !hasNextPage || isFetchingNextPage) return;
    void fetchNextPage();
    // fetchNextPage is referentially stable per query instance; pageCount
    // advancing (via `stopped`/`hasNextPage`) is what re-runs this until the
    // query is exhausted.
  }, [stopped, hasNextPage, isFetchingNextPage, fetchNextPage]);

  return {
    isTruncated: enabled && hasNextPage && (atCap || isFetchNextPageError),
  };
}

import { renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { MAX_PAGES, useDrainedPages } from "./useDrainedPages";

type Args = Parameters<typeof useDrainedPages>[0];

function args(overrides: Partial<Args> = {}): Args {
  return {
    pageCount: 1,
    hasNextPage: true,
    isFetchingNextPage: false,
    isFetchNextPageError: false,
    fetchNextPage: vi.fn(),
    ...overrides,
  };
}

describe("useDrainedPages", () => {
  it("pulls the next page while one remains", () => {
    const fetchNextPage = vi.fn();

    renderHook(() => useDrainedPages(args({ fetchNextPage })));

    expect(fetchNextPage).toHaveBeenCalledTimes(1);
  });

  it("waits for the page in flight rather than stacking requests", () => {
    const fetchNextPage = vi.fn();

    const { rerender } = renderHook((props: Args) => useDrainedPages(props), {
      initialProps: args({ isFetchingNextPage: true, fetchNextPage }),
    });
    rerender(args({ isFetchingNextPage: true, fetchNextPage }));

    expect(fetchNextPage).not.toHaveBeenCalled();
  });

  it("stops once the query is exhausted", () => {
    const fetchNextPage = vi.fn();

    renderHook(() =>
      useDrainedPages(args({ hasNextPage: false, fetchNextPage })),
    );

    expect(fetchNextPage).not.toHaveBeenCalled();
  });

  it("stops at the page cap and reports the set as truncated", () => {
    const fetchNextPage = vi.fn();

    const { result } = renderHook(() =>
      useDrainedPages(args({ pageCount: MAX_PAGES, fetchNextPage })),
    );

    expect(fetchNextPage).not.toHaveBeenCalled();
    expect(result.current.isTruncated).toBe(true);
  });

  // A failed fetch leaves hasNextPage true and pageCount unchanged, so without
  // an explicit stop the effect settles back into exactly the state that
  // triggered it and re-fires — a tight request loop with no backoff.
  it("does not retry after a page fetch fails", () => {
    const fetchNextPage = vi.fn();

    const { rerender } = renderHook((props: Args) => useDrainedPages(props), {
      initialProps: args({ isFetchingNextPage: true, fetchNextPage }),
    });
    // The failed fetch settles: still on page 1, still more to come.
    rerender(args({ isFetchNextPageError: true, fetchNextPage }));
    rerender(args({ isFetchNextPageError: true, fetchNextPage }));

    expect(fetchNextPage).not.toHaveBeenCalled();
  });

  it("reports a set cut short by a failure as truncated", () => {
    const { result } = renderHook(() =>
      useDrainedPages(args({ isFetchNextPageError: true })),
    );

    expect(result.current.isTruncated).toBe(true);
  });

  it("stays idle while the underlying query is disabled", () => {
    const fetchNextPage = vi.fn();

    const { result } = renderHook(() =>
      useDrainedPages(
        args({ enabled: false, pageCount: MAX_PAGES, fetchNextPage }),
      ),
    );

    expect(fetchNextPage).not.toHaveBeenCalled();
    expect(result.current.isTruncated).toBe(false);
  });

  it("reports a fully drained set as complete", () => {
    const { result } = renderHook(() =>
      useDrainedPages(args({ pageCount: MAX_PAGES, hasNextPage: false })),
    );

    expect(result.current.isTruncated).toBe(false);
  });
});

import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useDrainSkillPages } from "./use-drain-skill-pages";

describe("useDrainSkillPages", () => {
  it("fetches successive pages while search or filters are active", async () => {
    const fetchNextPage = vi.fn().mockResolvedValue(undefined);
    const { rerender } = renderHook(
      (props: {
        hasNextPage: boolean;
        isFetchingNextPage: boolean;
        isFetchNextPageError: boolean;
      }) => useDrainSkillPages({ active: true, fetchNextPage, ...props }),
      {
        initialProps: {
          hasNextPage: true,
          isFetchingNextPage: false,
          isFetchNextPageError: false,
        },
      },
    );

    await waitFor(() => expect(fetchNextPage).toHaveBeenCalledTimes(1));
    rerender({
      hasNextPage: true,
      isFetchingNextPage: true,
      isFetchNextPageError: false,
    });
    rerender({
      hasNextPage: true,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
    });
    await waitFor(() => expect(fetchNextPage).toHaveBeenCalledTimes(2));
    rerender({
      hasNextPage: false,
      isFetchingNextPage: false,
      isFetchNextPageError: false,
    });
    expect(fetchNextPage).toHaveBeenCalledTimes(2);
  });

  it("does not drain pages without active search or filters", () => {
    const fetchNextPage = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useDrainSkillPages({
        active: false,
        hasNextPage: true,
        isFetchingNextPage: false,
        isFetchNextPageError: false,
        fetchNextPage,
      }),
    );
    expect(fetchNextPage).not.toHaveBeenCalled();
  });

  it("stops automatic retries after the next page fails", () => {
    const fetchNextPage = vi.fn().mockResolvedValue(undefined);
    renderHook(() =>
      useDrainSkillPages({
        active: true,
        hasNextPage: true,
        isFetchingNextPage: false,
        isFetchNextPageError: true,
        fetchNextPage,
      }),
    );
    expect(fetchNextPage).not.toHaveBeenCalled();
  });
});

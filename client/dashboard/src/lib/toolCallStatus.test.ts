import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useStalePendingRerender } from "./toolCallStatus";

// The contract, stated apart from the implementation's constant: a call with
// no result reads Pending for ten minutes.
const TEN_MINUTES_MS = 10 * 60 * 1000;

describe("useStalePendingRerender", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    cleanup();
    vi.useRealTimers();
  });

  function renderCounting(startedAtMs: number | undefined) {
    let renders = 0;
    const hook = renderHook(() => {
      renders += 1;
      useStalePendingRerender(startedAtMs);
    });
    return { hook, renders: () => renders };
  }

  it("re-renders once at the ten-minute deadline", () => {
    const { renders } = renderCounting(Date.now() - TEN_MINUTES_MS + 1000);
    expect(renders()).toBe(1);
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(renders()).toBe(1);
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(renders()).toBe(2);
  });

  it("schedules nothing for a call already stale or not pending", () => {
    renderCounting(Date.now() - TEN_MINUTES_MS);
    renderCounting(undefined);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("clears its timer on unmount", () => {
    const { hook } = renderCounting(Date.now() - 1000);
    expect(vi.getTimerCount()).toBe(1);
    hook.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});

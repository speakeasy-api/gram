import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  pendingRestoredExpiries,
  resetRestoredFindings,
} from "./restored-findings-store";
import { RESTORE_HIDE_MS, useDismissFinding } from "./useDismissFinding";

const unmark = vi.fn(() => Promise.resolve({}));
const mark = vi.fn(() => Promise.resolve({}));

vi.mock("@gram/client/react-query/riskUnmarkResultsFalsePositive.js", () => ({
  useRiskUnmarkResultsFalsePositiveMutation: () => ({ mutateAsync: unmark }),
}));

vi.mock("@gram/client/react-query/riskMarkResultsFalsePositive.js", () => ({
  useRiskMarkResultsFalsePositiveMutation: () => ({ mutateAsync: mark }),
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

vi.mock("@/lib/toast-undo", () => ({ showUndoToast: vi.fn() }));

function wrapper({ children }: { children: ReactNode }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

function renderDismissFinding() {
  return renderHook(() => useDismissFinding(), { wrapper });
}

function isHeld(
  hook: ReturnType<typeof renderDismissFinding>,
  id: string,
): boolean {
  return hook.result.current.optimisticallyRestoredIds.has(id);
}

beforeEach(() => {
  vi.useFakeTimers();
  // The store is module-scoped, so it outlives a test unless reset.
  resetRestoredFindings();
  unmark.mockClear();
  mark.mockClear();
  unmark.mockImplementation(() => Promise.resolve({}));
});

afterEach(() => {
  resetRestoredFindings();
  vi.useRealTimers();
});

describe("useDismissFinding restore bookkeeping", () => {
  it("hides a restored finding until the mirror has had time to catch up", async () => {
    const hook = renderDismissFinding();

    await act(async () => {
      await hook.result.current.restore(["finding-1"]);
    });
    expect(isHeld(hook, "finding-1")).toBe(true);

    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS - 1000);
    });
    expect(isHeld(hook, "finding-1")).toBe(true);

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(isHeld(hook, "finding-1")).toBe(false);
    // The fired timer drops itself, so the map can't grow without bound.
    expect(pendingRestoredExpiries()).toBe(0);
  });

  it("holds the hide for the whole request and only then starts the clock", async () => {
    let settleRequest: (() => void) | undefined;
    unmark.mockImplementation(
      () =>
        new Promise<object>((resolve) => {
          settleRequest = () => resolve({});
        }),
    );
    const hook = renderDismissFinding();

    let restored: Promise<boolean> | undefined;
    act(() => {
      restored = hook.result.current.restore(["finding-1"]);
    });
    expect(isHeld(hook, "finding-1")).toBe(true);

    // A request slower than the whole expiry window must not un-hide the row:
    // the write hasn't landed, so the mirror is certainly still serving it.
    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS * 2);
    });
    expect(isHeld(hook, "finding-1")).toBe(true);

    await act(async () => {
      settleRequest?.();
      await restored;
    });
    expect(isHeld(hook, "finding-1")).toBe(true);

    // Only now does the window run.
    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS);
    });
    expect(isHeld(hook, "finding-1")).toBe(false);
  });

  it("lets a second restore of the same id supersede the first one's clock", async () => {
    const hook = renderDismissFinding();

    await act(async () => {
      await hook.result.current.restore(["finding-1"]);
    });
    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS - 1000);
    });

    await act(async () => {
      await hook.result.current.restore(["finding-1"]);
    });
    // Past the first restore's deadline — the second hold owns the id now, so
    // the stale timer must not cut it short.
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(isHeld(hook, "finding-1")).toBe(true);
    expect(pendingRestoredExpiries()).toBe(1);

    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS);
    });
    expect(isHeld(hook, "finding-1")).toBe(false);
  });

  it("stops hiding a finding suppressed again from another surface", async () => {
    // Two independent hook instances, as the suppressed section and the
    // signals drawer are.
    const listing = renderDismissFinding();
    const drawer = renderDismissFinding();

    await act(async () => {
      await listing.result.current.restore(["finding-1"]);
    });
    expect(isHeld(listing, "finding-1")).toBe(true);
    expect(isHeld(drawer, "finding-1")).toBe(true);

    await act(async () => {
      drawer.result.current.dismiss([{ id: "finding-1" } as never]);
    });

    // Suppressed again, so it belongs back on the listing immediately — not
    // RESTORE_HIDE_MS from now, and not only in the component that did it.
    expect(isHeld(drawer, "finding-1")).toBe(false);
    expect(isHeld(listing, "finding-1")).toBe(false);
    expect(pendingRestoredExpiries()).toBe(0);
  });

  it("puts a failed restore straight back on the listing", async () => {
    unmark.mockImplementation(() => Promise.reject(new Error("nope")));
    const hook = renderDismissFinding();

    await act(async () => {
      await hook.result.current.restore(["finding-1"]);
    });

    // Still suppressed server-side, so hiding it would misreport the state.
    expect(isHeld(hook, "finding-1")).toBe(false);
    expect(pendingRestoredExpiries()).toBe(0);
  });
});

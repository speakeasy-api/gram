import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
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

beforeEach(() => {
  vi.useFakeTimers();
  unmark.mockClear();
  mark.mockClear();
  unmark.mockImplementation(() => Promise.resolve({}));
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useDismissFinding restore bookkeeping", () => {
  it("hides a restored finding, then stops once the mirror has had time to catch up", async () => {
    const { result } = renderDismissFinding();

    await act(async () => {
      await result.current.restore(["finding-1"]);
    });
    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      true,
    );

    // Well short of the window: the mirror may still be serving the old row.
    act(() => {
      vi.advanceTimersByTime(RESTORE_HIDE_MS - 1000);
    });
    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      true,
    );

    act(() => {
      vi.advanceTimersByTime(1000);
    });
    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      false,
    );
  });

  it("stops hiding a finding the moment it is suppressed again", async () => {
    const { result } = renderDismissFinding();

    await act(async () => {
      await result.current.restore(["finding-1"]);
    });
    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      true,
    );

    // Re-suppressed well inside the hide window: it belongs back on the
    // suppressed listing immediately, not RESTORE_HIDE_MS from now.
    await act(async () => {
      result.current.dismiss([{ id: "finding-1" } as never]);
    });

    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      false,
    );
  });

  it("puts a failed restore straight back on the listing", async () => {
    unmark.mockImplementation(() => Promise.reject(new Error("nope")));
    const { result } = renderDismissFinding();

    await act(async () => {
      await result.current.restore(["finding-1"]);
    });

    // Still suppressed server-side, so hiding it would misreport the state.
    expect(result.current.optimisticallyRestoredIds.has("finding-1")).toBe(
      false,
    );
  });
});

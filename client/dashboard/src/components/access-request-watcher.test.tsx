import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

type Request = {
  id: string;
  scope: string;
  status: string;
  grantExpiresAt?: Date;
};

// The viewer's own requests as the polled list returns them, the polling
// options the watcher passed, and the rollout.
const state = vi.hoisted(() => ({
  on: true,
  requests: [] as Request[],
  options: undefined as
    | {
        refetchInterval?: (query: {
          state: { data?: { requests: Request[] } };
        }) => number | false;
      }
    | undefined,
}));

vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => ({ status: state.on ? "enabled" : "disabled" }),
}));
vi.mock("@gram/client/react-query/accessRequests.js", () => ({
  useAccessRequests: (
    _request: unknown,
    _security: unknown,
    options: typeof state.options,
  ) => {
    state.options = options;
    return { data: { requests: [...state.requests] } };
  },
  invalidateAllAccessRequests: () => Promise.resolve(),
}));
vi.mock("./temporary-access-helpers", () => ({
  invalidateTemporaryAccess: vi.fn(),
}));
const toast = vi.hoisted(() => ({
  success: vi.fn(),
  error: vi.fn(),
  info: vi.fn(),
}));
vi.mock("sonner", () => ({ toast }));
const invalidateQueries = vi.hoisted(() => vi.fn(() => Promise.resolve()));
vi.mock("@tanstack/react-query", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@tanstack/react-query")>()),
  useQueryClient: () => ({ invalidateQueries }),
}));

import {
  ACCESS_REQUEST_POLL_MS,
  AccessRequestWatcher,
} from "./access-request-watcher";

afterEach(() => {
  cleanup();
  state.on = true;
  state.requests = [];
  state.options = undefined;
  toast.success.mockClear();
  toast.error.mockClear();
  toast.info.mockClear();
  invalidateQueries.mockClear();
});

const pending: Request = { id: "r1", scope: "mcp:write", status: "pending" };

/** Whether every query was refetched, not only the access ones. */
function refetchedEverything(): boolean {
  return invalidateQueries.mock.calls.some(
    (call: unknown[]) => call.length === 0,
  );
}

describe("AccessRequestWatcher", () => {
  it("polls only while one of the viewer's requests is pending", () => {
    render(<AccessRequestWatcher />);
    const interval = state.options?.refetchInterval;

    expect(interval?.({ state: { data: { requests: [pending] } } })).toBe(
      ACCESS_REQUEST_POLL_MS,
    );
    expect(
      interval?.({
        state: { data: { requests: [{ ...pending, status: "denied" }] } },
      }),
    ).toBe(false);
  });

  it("tells the viewer once a request is approved and refetches the page", () => {
    state.requests = [pending];
    const { rerender } = render(<AccessRequestWatcher />);

    state.requests = [
      {
        ...pending,
        status: "approved",
        grantExpiresAt: new Date(2026, 10, 20, 14, 30),
      },
    ];
    rerender(<AccessRequestWatcher />);

    expect(toast.success).toHaveBeenCalledTimes(1);
    expect(toast.success).toHaveBeenCalledWith("Access approved until 14:30.");
    // Lists fetched before the approval were filtered, not refused.
    expect(refetchedEverything()).toBe(true);
  });

  it("tells the viewer a request was denied without refetching the page", () => {
    state.requests = [pending];
    const { rerender } = render(<AccessRequestWatcher />);

    state.requests = [{ ...pending, status: "denied" }];
    rerender(<AccessRequestWatcher />);

    expect(toast.error).toHaveBeenCalledWith("Your access request was denied.");
    expect(refetchedEverything()).toBe(false);
  });

  it("stays quiet about requests decided before the app loaded", () => {
    state.requests = [{ ...pending, status: "approved" }];
    render(<AccessRequestWatcher />);

    expect(toast.success).not.toHaveBeenCalled();
    expect(invalidateQueries).not.toHaveBeenCalled();
  });

  it("does nothing with the rollout off", () => {
    state.on = false;
    render(<AccessRequestWatcher />);

    expect(state.options).toBeUndefined();
  });
});

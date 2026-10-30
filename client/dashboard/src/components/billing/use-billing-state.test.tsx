import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Gram } from "@gram/client";
import { HTTPClient, type Fetcher } from "@gram/client/lib/http.js";
import { GramProvider } from "@gram/client/react-query/_context.js";
import { queryKeyGetStripeSubscription } from "@gram/client/react-query/getStripeSubscription.js";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useBillingState } from "./use-billing-state";

const mocks = vi.hoisted(() => ({
  session: vi.fn(),
  flag: vi.fn(),
  hasScope: vi.fn(),
}));
vi.mock("@/contexts/Auth", () => ({ useSession: () => mocks.session() }));
vi.mock("@/hooks/useFeatureFlag", () => ({
  useFeatureFlag: () => mocks.flag(),
}));
vi.mock("@/hooks/useRBAC", () => ({
  useRBAC: () => ({ hasScope: mocks.hasScope }),
}));
const DAY = 86_400_000;
const activeTrial = {
  startedAt: new Date(Date.now() - DAY),
  endsAt: new Date(Date.now() + DAY),
};
const expiredTrial = {
  startedAt: new Date(Date.now() - 2 * DAY),
  endsAt: new Date(Date.now() - DAY),
};
const session = {
  activeOrganizationId: "org-billing-test",
  rawGramAccountType: "payg",
  whitelisted: true,
  hasActiveSubscription: false,
  trial: null,
};
const key = queryKeyGetStripeSubscription({});
const clients: QueryClient[] = [];
function subscriptionResponse(status = "active") {
  return Response.json({
    status,
    cancel_at_period_end: false,
    payment_failed: false,
    current_period_start: "2026-01-01T00:00:00Z",
    current_period_end: "2026-02-01T00:00:00Z",
  });
}
function errorResponse(status: number) {
  return Response.json(
    {
      fault: status >= 500,
      id: "test-error",
      message: "Subscription unavailable",
      name: "subscription_error",
      temporary: false,
      timeout: false,
    },
    { status },
  );
}
function deferred() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>((done) => {
    resolve = done;
  });
  return { promise, resolve };
}
function setup() {
  const fetcher = vi.fn<Fetcher>();
  const sdk = new Gram({
    serverURL: "https://billing.example",
    httpClient: new HTTPClient({ fetcher }),
    retryConfig: { strategy: "none" },
  });
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  clients.push(client);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <GramProvider client={sdk}>{children}</GramProvider>
    </QueryClientProvider>
  );
  return { client, fetcher, wrapper };
}
beforeEach(() => {
  vi.clearAllMocks();
  mocks.session.mockReturnValue(session);
  mocks.flag.mockReturnValue({ status: "enabled" });
  mocks.hasScope.mockReturnValue(true);
});
afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  vi.useRealTimers();
});

describe("useBillingState", () => {
  it.each([
    ["free", null, false, true],
    ["base", null, false, true],
    ["free", null, true, false],
    ["base", null, true, false],
    ["payg", null, true, true],
    ["enterprise", null, true, false],
    ["enterprise", activeTrial, true, true],
    ["enterprise", expiredTrial, true, false],
  ])(
    "resolves billing eligibility for %s, trial=%j, subscribed=%s",
    async (account, trial, subscribed, eligible) => {
      mocks.session.mockReturnValue({
        ...session,
        rawGramAccountType: account,
        trial,
        hasActiveSubscription: subscribed,
      });
      const { wrapper, fetcher } = setup();
      fetcher.mockResolvedValue(errorResponse(404));
      const { result } = renderHook(() => useBillingState(), { wrapper });
      expect(result.current.checkoutEligible).toBe(eligible);
      await waitFor(() =>
        expect(result.current.status).toBe(
          eligible ? "needs-setup" : "unavailable",
        ),
      );
      expect(result.current.trialLifecycle).toBe(
        trial === activeTrial
          ? "active"
          : trial === expiredTrial
            ? "expired"
            : "none",
      );
      expect(result.current.checkoutConfirmed).toBe(false);
      expect(fetcher).toHaveBeenCalledTimes(eligible ? 1 : 0);
    },
  );
  it.each(["disabled", "loading", "error"])(
    "blocks active trial checkout when the flag is %s",
    (status) => {
      mocks.flag.mockReturnValue({ status });
      mocks.session.mockReturnValue({ ...session, trial: activeTrial });
      const { wrapper, fetcher } = setup();
      const { result } = renderHook(() => useBillingState(), { wrapper });
      expect(result.current).toMatchObject({
        status: "unavailable",
        checkoutEligible: false,
        checkoutConfirmed: false,
      });
      expect(fetcher).not.toHaveBeenCalled();
    },
  );
  it.each([
    { scopes: [] },
    { scopes: ["org:read"] },
    { scopes: ["org:admin"] },
  ])("requires admin scope for trial checkout ($scopes)", ({ scopes }) => {
    mocks.hasScope.mockImplementation((value: string) =>
      scopes.includes(value),
    );
    mocks.session.mockReturnValue({ ...session, trial: activeTrial });
    const { wrapper, fetcher } = setup();
    fetcher.mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useBillingState(), { wrapper });
    expect(result.current.checkoutEligible).toBe(scopes.includes("org:admin"));
    expect(mocks.hasScope).toHaveBeenCalledWith("org:admin");
  });
  it("retains PAYG management for read-only viewers without the rollout", async () => {
    mocks.flag.mockReturnValue({ status: "disabled" });
    mocks.hasScope.mockImplementation((scope: string) => scope === "org:read");
    const { wrapper, fetcher } = setup();
    fetcher.mockResolvedValue(subscriptionResponse());
    const { result } = renderHook(() => useBillingState(), { wrapper });
    await waitFor(() => expect(result.current.status).toBe("manage"));
    expect(result.current.checkoutEligible).toBe(false);
  });
  it("does not read PAYG subscriptions without organization scope", () => {
    mocks.hasScope.mockReturnValue(false);
    const { wrapper, fetcher } = setup();
    const { result } = renderHook(() => useBillingState(), { wrapper });
    expect(result.current.status).toBe("unavailable");
    expect(fetcher).not.toHaveBeenCalled();
  });
  it.each([null, expiredTrial])(
    "permits gated lockout checkout only on setup (%j)",
    async (trial) => {
      mocks.session.mockReturnValue({
        ...session,
        rawGramAccountType: "enterprise",
        whitelisted: false,
        trial,
      });
      const { wrapper, fetcher } = setup();
      fetcher.mockResolvedValue(errorResponse(404));
      const { result, rerender } = renderHook(
        ({ surface }: { surface: "billing" | "setup" }) =>
          useBillingState({ surface }),
        { wrapper, initialProps: { surface: "billing" } },
      );
      expect(result.current.status).toBe("unavailable");
      rerender({ surface: "setup" });
      await waitFor(() => expect(result.current.status).toBe("needs-setup"));
      expect(result.current.checkoutEligible).toBe(true);
    },
  );
  it.each([
    "active",
    "trialing",
    "past_due",
    "unpaid",
    "paused",
    "canceled",
    "incomplete",
    "incomplete_expired",
  ])(
    "only confirms fresh active/trialing subscriptions: %s",
    async (status) => {
      const { wrapper, fetcher } = setup();
      fetcher.mockResolvedValue(subscriptionResponse(status));
      const { result } = renderHook(
        () => useBillingState({ surface: "setup" }),
        { wrapper },
      );
      await waitFor(() => expect(result.current.fresh).toBe(true));
      expect(result.current.status).toBe("manage");
      expect(result.current.subscription.data?.status).toBe(status);
      expect(result.current.checkoutConfirmed).toBe(
        status === "active" || status === "trialing",
      );
    },
  );
  it.each([404, 500])(
    "prioritizes HTTP %s correctly over stale data",
    async (code) => {
      const { wrapper, fetcher } = setup();
      fetcher
        .mockResolvedValueOnce(subscriptionResponse())
        .mockResolvedValueOnce(errorResponse(code));
      const { result } = renderHook(() => useBillingState(), { wrapper });
      await waitFor(() => expect(result.current.checkoutConfirmed).toBe(true));
      await act(async () => {
        await result.current.subscription.refetch();
      });
      await waitFor(() =>
        expect(result.current.subscription.error).not.toBeNull(),
      );
      expect(result.current.subscription.data?.status).toBe("active");
      expect(result.current.status).toBe(
        code === 404 ? "needs-setup" : "manage",
      );
      expect(result.current.checkoutConfirmed).toBe(false);
    },
  );
  it("returns error for a non-404 failure with no data", async () => {
    const { wrapper, fetcher } = setup();
    fetcher.mockResolvedValue(errorResponse(500));
    const { result } = renderHook(() => useBillingState(), { wrapper });
    await waitFor(() => expect(result.current.status).toBe("error"));
    expect(result.current.checkoutConfirmed).toBe(false);
  });
  it("does not offer setup on 404 when checkout is unavailable", async () => {
    mocks.flag.mockReturnValue({ status: "disabled" });
    const { wrapper, fetcher } = setup();
    fetcher.mockResolvedValue(errorResponse(404));
    const { result } = renderHook(() => useBillingState(), { wrapper });
    await waitFor(() =>
      expect(result.current.subscription.error).not.toBeNull(),
    );
    expect(result.current.status).toBe("unavailable");
  });
  it("retains management but clears confirmation during refetch", async () => {
    const { wrapper, fetcher } = setup();
    const pending = deferred();
    fetcher
      .mockResolvedValueOnce(subscriptionResponse())
      .mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useBillingState(), { wrapper });
    expect(result.current).toMatchObject({
      status: "loading",
      fresh: false,
      checkoutConfirmed: false,
    });
    await waitFor(() => expect(result.current.checkoutConfirmed).toBe(true));
    act(() => {
      void result.current.subscription.refetch();
    });
    await waitFor(() =>
      expect(result.current.subscription.isFetching).toBe(true),
    );
    expect(result.current).toMatchObject({
      status: "manage",
      fresh: false,
      checkoutConfirmed: false,
    });
    await act(async () => pending.resolve(subscriptionResponse("trialing")));
    await waitFor(() => expect(result.current.checkoutConfirmed).toBe(true));
  });
  it("recomputes eligibility on organization switches without forking the SDK query key", async () => {
    const { wrapper, fetcher, client } = setup();
    fetcher.mockResolvedValueOnce(subscriptionResponse());
    const { result, rerender } = renderHook(() => useBillingState(), {
      wrapper,
    });
    await waitFor(() => expect(result.current.checkoutConfirmed).toBe(true));
    mocks.session.mockReturnValue({
      ...session,
      activeOrganizationId: "org-other-test",
      rawGramAccountType: "enterprise",
    });
    rerender();
    expect(result.current).toMatchObject({
      status: "unavailable",
      checkoutEligible: false,
      checkoutConfirmed: false,
    });
    expect(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey),
    ).toEqual([key]);
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it("polls every two seconds only while setup is confirming and stops on unmount", async () => {
    vi.useFakeTimers();
    const { wrapper, fetcher } = setup();
    fetcher.mockImplementation(async () => subscriptionResponse());
    const billing = renderHook(() => useBillingState(), { wrapper });
    const { rerender, unmount } = renderHook(
      ({ confirming }) => useBillingState({ surface: "setup", confirming }),
      { wrapper, initialProps: { confirming: false } },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(fetcher).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000);
    });
    expect(fetcher).toHaveBeenCalledTimes(1);

    rerender({ confirming: true });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1999);
    });
    expect(fetcher).toHaveBeenCalledTimes(1);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(1);
    });
    expect(fetcher).toHaveBeenCalledTimes(2);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(fetcher).toHaveBeenCalledTimes(3);

    rerender({ confirming: false });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000);
    });
    expect(fetcher).toHaveBeenCalledTimes(3);

    rerender({ confirming: true });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(fetcher).toHaveBeenCalledTimes(4);
    unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(4000);
    });
    expect(fetcher).toHaveBeenCalledTimes(4);
    expect(billing.result.current.status).toBe("manage");
  });

  it("shares the SDK query identity and refreshes cached billing data on setup mount", async () => {
    const { wrapper, fetcher, client } = setup();
    fetcher.mockResolvedValueOnce(subscriptionResponse());
    const billing = renderHook(() => useBillingState(), { wrapper });
    await waitFor(() =>
      expect(billing.result.current.checkoutConfirmed).toBe(true),
    );
    billing.unmount();
    const pending = deferred();
    fetcher.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(
      () => useBillingState({ surface: "setup", confirming: true }),
      { wrapper },
    );
    expect(result.current).toMatchObject({
      status: "manage",
      fresh: false,
      checkoutConfirmed: false,
    });
    expect(
      client
        .getQueryCache()
        .getAll()
        .map((query) => query.queryKey),
    ).toEqual([key]);
    await act(async () => pending.resolve(subscriptionResponse()));
    await waitFor(() => expect(result.current.checkoutConfirmed).toBe(true));
    expect(fetcher).toHaveBeenCalledTimes(2);
  });
});

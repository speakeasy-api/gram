import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  subscription: vi.fn(),
  summary: vi.fn(),
}));

// The shared subscription read is mocked at the wrapper: what the estimate does
// with the live Stripe state is this file's subject, and the wrapper's own
// query options are covered by `use-stripe-subscription.test.ts`.
vi.mock("@/components/billing/use-stripe-subscription", () => ({
  useStripeSubscription: () => mocks.subscription(),
}));

// The hook options are part of what's under test — the estimate has to be
// conditioned on the gate and opt out of the shared throwOnError — so the
// arguments are forwarded, not dropped.
vi.mock("@gram/client/react-query/getPaygBillingSummary.js", () => ({
  useGetPaygBillingSummary: (...args: unknown[]) => mocks.summary(...args),
}));

import { PaygCycleEstimate } from "./payg-cycle-estimate";

// The subscription's anchor is read against the real clock, so it is pinned far
// enough either side of now that no run can land on the wrong side of it.
const UNOPENED_PERIOD_START = new Date("2999-01-01T00:00:00.000Z");

// The dates the summary itself reports, which are what gets rendered — and the
// subscription anchor the estimate matches the summary against. Midday UTC so
// a formatted day can't slide either side of the date line in whichever time
// zone the tests happen to run in.
const PERIOD_START = new Date("2026-08-01T12:00:00.000Z");
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");

type Summary = {
  periodStart: Date;
  periodEnd: Date;
  tumTokens: number | bigint;
  tumUnitPriceUsd: string;
  tumCostUsd: string;
  otherInferenceSpendUsd: string;
  estimatedTotalUsd: string;
  recordedThrough?: { toString: () => string };
};

function summaryData(overrides: Partial<Summary> = {}): Summary {
  return {
    periodStart: PERIOD_START,
    periodEnd: PERIOD_END,
    tumTokens: 1234567,
    tumUnitPriceUsd: "0.00000015",
    tumCostUsd: "0.19",
    otherInferenceSpendUsd: "4.10",
    estimatedTotalUsd: "4.29",
    recordedThrough: "2026-08-15",
    ...overrides,
  };
}

type Subscription = { status: string; currentPeriodStart: Date };

/**
 * The live Stripe subscription the estimate gates on. Passing `undefined` is a
 * read that hasn't answered yet, which is why this takes it explicitly rather
 * than through a default.
 */
function stripeSubscription(data: Subscription | undefined) {
  mocks.subscription.mockReturnValue({ data });
}

/**
 * A subscription Stripe is billing, on a period that has already opened and
 * whose anchor matches the summary's own period.
 */
function billingSubscription(status = "active") {
  stripeSubscription({ status, currentPeriodStart: PERIOD_START });
}

type SummaryQueryState = { data?: Summary | undefined; isError?: boolean };

/** The refetch the staleness guard fires once per new subscription anchor. */
const refetchSummary = vi.fn();

function summaryQuery({ data, isError = false }: SummaryQueryState = {}) {
  mocks.summary.mockReturnValue({ data, isError, refetch: refetchSummary });
}

/** The options the estimate passed to its generated query hook. */
function summaryOptions(): { enabled?: boolean; throwOnError?: boolean } {
  const call = mocks.summary.mock.calls[0] as unknown[] | undefined;
  return (call?.[2] ?? {}) as { enabled?: boolean; throwOnError?: boolean };
}

const estimatedTotal = () => screen.queryByText("$4.29");

describe("PaygCycleEstimate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    billingSubscription();
    summaryQuery({ data: summaryData() });
  });

  afterEach(cleanup);

  it("renders the cycle's billable usage and estimated invoice", () => {
    render(<PaygCycleEstimate />);

    expect(screen.getByText("1,234,567")).toBeTruthy();
    expect(screen.getByText("$4.10")).toBeTruthy();
    expect(estimatedTotal()).not.toBeNull();
    expect(
      screen.getByText("$0.19 at a flat $0.00000015 per token."),
    ).toBeTruthy();
  });

  it("names the cycle's own period dates", () => {
    render(<PaygCycleEstimate />);

    expect(
      screen.getByText(/August 1, 2026 to September 1, 2026/),
    ).toBeTruthy();
  });

  it("names all billable Gram-managed inference", () => {
    render(<PaygCycleEstimate />);

    expect(screen.getByText("Inference spend")).toBeTruthy();
    expect(
      screen.getByText(/customer-facing and platform-initiated inference/),
    ).toBeTruthy();
  });

  // The cutoff is the whole point of that figure: today's usage is not in it.
  it("names the recorded-through cutoff on the inference spend", () => {
    render(<PaygCycleEstimate />);

    expect(
      screen.getByText(/Completed days through August 15, 2026/),
    ).toBeTruthy();
  });

  it("says so when no completed day of inference spend has landed yet", () => {
    summaryQuery({ data: summaryData({ recordedThrough: undefined }) });

    render(<PaygCycleEstimate />);

    expect(
      screen.getByText(/No completed day has been recorded in this cycle yet/),
    ).toBeTruthy();
  });

  // An invoice that finalizes days after the cycle closes must not be presented
  // as the final number.
  it("warns that the invoice finalizes up to 72 hours after the cycle", () => {
    render(<PaygCycleEstimate />);

    expect(
      screen.getByText(/up to 72 hours after the cycle ends/),
    ).toBeTruthy();
  });

  // The total stops at the same cutoff its inference component does, so it has
  // to say where it stops rather than reading as a complete cycle-to-date
  // figure.
  it("carries the recorded-through cutoff onto the estimated total", () => {
    render(<PaygCycleEstimate />);

    expect(
      screen.getByText(/Inference spend counted through August 15, 2026/),
    ).toBeTruthy();
  });

  // int64 token counts outrun a double, so the count has to survive as digits.
  it("renders a token count beyond double precision exactly", () => {
    summaryQuery({ data: summaryData({ tumTokens: 9007199254740993n }) });

    render(<PaygCycleEstimate />);

    expect(screen.getByText("9,007,199,254,740,993")).toBeTruthy();
  });

  it("renders exact decimal money without rounding it to cents", () => {
    summaryQuery({
      data: summaryData({
        tumCostUsd: "1234.5678",
        estimatedTotalUsd: "1238.6678",
      }),
    });

    render(<PaygCycleEstimate />);

    expect(screen.getByText("$1,238.6678")).toBeTruthy();
    expect(screen.getByText(/\$1,234\.5678 at a flat/)).toBeTruthy();
  });

  describe("query conditioning", () => {
    it("enables the estimate once the period has opened", () => {
      render(<PaygCycleEstimate />);

      expect(summaryOptions().enabled).toBe(true);
    });

    // The billing page must survive a failing estimate, so the read opts out of
    // the shared error boundary.
    it("keeps the estimate's failures off the app error boundary", () => {
      render(<PaygCycleEstimate />);

      expect(summaryOptions().throwOnError).toBe(false);
    });

    it.each<[string, string]>([
      ["a trialing subscription", "trialing"],
      ["a canceled subscription", "canceled"],
      ["an incomplete subscription", "incomplete"],
    ])("never fires the estimate for %s", (_label, status) => {
      billingSubscription(status);
      summaryQuery();

      render(<PaygCycleEstimate />);

      expect(summaryOptions().enabled).toBe(false);
      expect(estimatedTotal()).toBeNull();
    });

    it("never fires the estimate before the period anchor", () => {
      stripeSubscription({
        status: "active",
        currentPeriodStart: UNOPENED_PERIOD_START,
      });
      summaryQuery();

      render(<PaygCycleEstimate />);

      expect(summaryOptions().enabled).toBe(false);
      expect(estimatedTotal()).toBeNull();
    });

    it("never fires the estimate while the subscription is unknown", () => {
      stripeSubscription(undefined);
      summaryQuery();

      render(<PaygCycleEstimate />);

      expect(summaryOptions().enabled).toBe(false);
    });
  });

  // At a cycle boundary the live subscription moves onto the new period while
  // the cache still holds the prior cycle's summary — those figures must not
  // read as current, and the fresh summary has to be asked for.
  it("keeps a cached prior-cycle summary loading and refetches it once", () => {
    const priorCycle = summaryData({
      periodStart: new Date("2026-07-01T12:00:00.000Z"),
      periodEnd: PERIOD_START,
    });
    summaryQuery({ data: priorCycle });

    const { rerender } = render(<PaygCycleEstimate />);

    expect(estimatedTotal()).toBeNull();
    expect(screen.queryByText(/Current billing cycle/)).toBeNull();
    expect(refetchSummary).toHaveBeenCalledTimes(1);

    // A new refetch identity re-runs the guard's effect; the still-mismatched
    // anchor must not be asked for again — the mismatch can also mean the
    // subscription read is the stale one, and refetching until the two agree
    // would poll the endpoint in a loop.
    const laterRefetch = vi.fn();
    mocks.summary.mockReturnValue({
      data: priorCycle,
      isError: false,
      refetch: laterRefetch,
    });
    rerender(<PaygCycleEstimate />);

    expect(laterRefetch).not.toHaveBeenCalled();
    expect(refetchSummary).toHaveBeenCalledTimes(1);
  });

  // A stale cached summary on a subscription that stopped billing has no cycle
  // to refetch for — the disabled query must not be forced to hit the billing
  // endpoint.
  it("never refetches a stale summary for a non-billing subscription", () => {
    billingSubscription("canceled");
    summaryQuery({
      data: summaryData({
        periodStart: new Date("2026-07-01T12:00:00.000Z"),
        periodEnd: PERIOD_START,
      }),
    });

    render(<PaygCycleEstimate />);

    expect(refetchSummary).not.toHaveBeenCalled();
  });

  // A 404, a conflict, or an outage all leave the page without an estimate
  // rather than with a failure notice beside the plan's real state.
  it("shows nothing when the estimate can't be loaded", () => {
    summaryQuery({ isError: true });

    const { container } = render(<PaygCycleEstimate />);

    expect(estimatedTotal()).toBeNull();
    expect(screen.queryByText(/72 hours/)).toBeNull();
    expect(container.childElementCount).toBe(0);
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render as rtlRender, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { ProductTier } from "@/hooks/useProductTier";

const mocks = vi.hoisted(() => ({
  productTier: vi.fn(),
  subscription: vi.fn(),
  fetchSummary: vi.fn(),
}));

vi.mock("@/hooks/useProductTier", () => ({
  useProductTier: () => mocks.productTier() as ProductTier,
}));

// The shared subscription read is mocked at the wrapper: what the estimate does
// with the live Stripe state is this file's subject, and the wrapper's own
// query options are covered by `use-stripe-subscription.test.ts`.
vi.mock("@/components/billing/use-stripe-subscription", () => ({
  useStripeSubscription: () => mocks.subscription(),
}));

vi.mock("@gram/client/react-query/_context.js", () => ({
  useGramContext: () => ({}),
}));

// The estimate composes the generated query pieces itself so it can key the
// cache on the subscription's period anchor. The real react-query client runs
// in these tests, so the anchor keying, the enabled gate, and the local error
// handling are exercised as behavior rather than as forwarded options.
vi.mock("@gram/client/react-query/getPaygBillingSummary.js", () => ({
  buildGetPaygBillingSummaryQuery: () => ({
    queryKey: ["payg-billing-summary"],
    queryFn: mocks.fetchSummary,
  }),
  queryKeyGetPaygBillingSummary: () => ["payg-billing-summary"],
}));

import { PaygCycleEstimate } from "./payg-cycle-estimate";

// The subscription's anchor is read against the real clock, so it is pinned far
// enough either side of now that no run can land on the wrong side of it.
const UNOPENED_PERIOD_START = new Date("2999-01-01T00:00:00.000Z");

// The dates the summary itself reports, which are what gets rendered — and the
// subscription anchor the query key carries. Midday UTC so a formatted day
// can't slide either side of the date line in whichever time zone the tests
// happen to run in.
const PERIOD_START = new Date("2026-08-01T12:00:00.000Z");
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");
// A later anchor that is still in the past, so the rolled-over cycle's period
// has opened under the real clock the gate reads.
const NEXT_ANCHOR = new Date("2026-08-15T12:00:00.000Z");

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

function summaryResolves(data: Summary) {
  mocks.fetchSummary.mockResolvedValue(data);
}

/** One shared client per test so anchor-keyed entries share a real cache. */
function render(ui: ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const wrap = (node: ReactNode) => (
    <QueryClientProvider client={client}>{node}</QueryClientProvider>
  );
  const result = rtlRender(wrap(ui));
  return {
    ...result,
    rerender: (node: ReactNode) => result.rerender(wrap(node)),
  };
}

const estimatedTotal = () => screen.queryByText("$4.29");

describe("PaygCycleEstimate", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.productTier.mockReturnValue("payg");
    billingSubscription();
    summaryResolves(summaryData());
  });

  afterEach(cleanup);

  it("renders the cycle's billable usage and estimated invoice", async () => {
    render(<PaygCycleEstimate />);

    expect(await screen.findByText("1,234,567")).toBeTruthy();
    expect(screen.getByText("$4.10")).toBeTruthy();
    expect(estimatedTotal()).not.toBeNull();
    expect(
      screen.getByText("$0.19 at a flat $0.00000015 per token."),
    ).toBeTruthy();
  });

  it("names the cycle's own period dates", async () => {
    render(<PaygCycleEstimate />);

    expect(
      await screen.findByText(/August 1, 2026 to September 1, 2026/),
    ).toBeTruthy();
  });

  it("names all billable Gram-managed inference", async () => {
    render(<PaygCycleEstimate />);

    expect(await screen.findByText("Inference spend")).toBeTruthy();
    expect(
      screen.getByText(/customer-facing and platform-initiated inference/),
    ).toBeTruthy();
  });

  // The cutoff is the whole point of that figure: today's usage is not in it.
  it("names the recorded-through cutoff on the inference spend", async () => {
    render(<PaygCycleEstimate />);

    expect(
      await screen.findByText(/Completed days through August 15, 2026/),
    ).toBeTruthy();
  });

  it("says so when no completed day of inference spend has landed yet", async () => {
    summaryResolves(summaryData({ recordedThrough: undefined }));

    render(<PaygCycleEstimate />);

    expect(
      await screen.findByText(
        /No completed day has been recorded in this cycle yet/,
      ),
    ).toBeTruthy();
  });

  // An invoice that finalizes days after the cycle closes must not be presented
  // as the final number.
  it("warns that the invoice finalizes up to 72 hours after the cycle", async () => {
    render(<PaygCycleEstimate />);

    expect(
      await screen.findByText(/up to 72 hours after the cycle ends/),
    ).toBeTruthy();
  });

  // The total stops at the same cutoff its inference component does, so it has
  // to say where it stops rather than reading as a complete cycle-to-date
  // figure.
  it("carries the recorded-through cutoff onto the estimated total", async () => {
    render(<PaygCycleEstimate />);

    expect(
      await screen.findByText(
        /Inference spend counted through August 15, 2026/,
      ),
    ).toBeTruthy();
  });

  // int64 token counts outrun a double, so the count has to survive as digits.
  it("renders a token count beyond double precision exactly", async () => {
    summaryResolves(summaryData({ tumTokens: 9007199254740993n }));

    render(<PaygCycleEstimate />);

    expect(await screen.findByText("9,007,199,254,740,993")).toBeTruthy();
  });

  it("renders exact decimal money without rounding it to cents", async () => {
    summaryResolves(
      summaryData({
        tumCostUsd: "1234.5678",
        estimatedTotalUsd: "1238.6678",
      }),
    );

    render(<PaygCycleEstimate />);

    expect(await screen.findByText("$1,238.6678")).toBeTruthy();
    expect(screen.getByText(/\$1,234\.5678 at a flat/)).toBeTruthy();
  });

  describe("query conditioning", () => {
    it("fetches the estimate once the period has opened", async () => {
      render(<PaygCycleEstimate />);

      await screen.findByText("$4.29");
      expect(mocks.fetchSummary).toHaveBeenCalledTimes(1);
    });

    it.each<[string, string]>([
      ["a trialing subscription", "trialing"],
      ["a canceled subscription", "canceled"],
      ["an incomplete subscription", "incomplete"],
    ])("never fires the estimate for %s", (_label, status) => {
      billingSubscription(status);

      const { container } = render(<PaygCycleEstimate />);

      expect(mocks.fetchSummary).not.toHaveBeenCalled();
      expect(container.childElementCount).toBe(0);
    });

    it("never fires the estimate before the period anchor", () => {
      stripeSubscription({
        status: "active",
        currentPeriodStart: UNOPENED_PERIOD_START,
      });

      const { container } = render(<PaygCycleEstimate />);

      expect(mocks.fetchSummary).not.toHaveBeenCalled();
      expect(container.childElementCount).toBe(0);
    });

    it("never fires the estimate while the subscription is unknown", () => {
      stripeSubscription(undefined);

      render(<PaygCycleEstimate />);

      expect(mocks.fetchSummary).not.toHaveBeenCalled();
    });
  });

  // At a cycle boundary the live subscription moves onto the new period. The
  // anchor is part of the query key, so the prior cycle's cached summary is a
  // different entry: the new cycle loads fresh instead of rendering last
  // cycle's figures as current.
  it("keys the summary on the cycle anchor so a new cycle never shows the prior cycle's figures", async () => {
    const { rerender } = render(<PaygCycleEstimate />);
    await screen.findByText("$4.29");

    // The subscription rolls onto the next period; the summary endpoint now
    // reports that cycle.
    stripeSubscription({ status: "active", currentPeriodStart: NEXT_ANCHOR });
    summaryResolves(
      summaryData({
        periodStart: NEXT_ANCHOR,
        periodEnd: PERIOD_END,
        estimatedTotalUsd: "0.02",
      }),
    );
    rerender(<PaygCycleEstimate />);

    // The prior cycle's figures are gone immediately — a fresh entry loads.
    expect(estimatedTotal()).toBeNull();
    expect(await screen.findByText("$0.02")).toBeTruthy();
    expect(
      screen.getByText(/August 15, 2026 to September 1, 2026/),
    ).toBeTruthy();
    expect(mocks.fetchSummary).toHaveBeenCalledTimes(2);
  });

  // A 404, a conflict, or an outage all leave the page without an estimate
  // rather than with a failure notice beside the plan's real state — and the
  // failure stays out of the app error boundary, which would otherwise take
  // the whole billing page down over one informational row.
  it("shows nothing when the estimate can't be loaded", async () => {
    mocks.fetchSummary.mockRejectedValue(new Error("conflict"));

    const { container } = render(<PaygCycleEstimate />);

    await vi.waitFor(() => expect(container.childElementCount).toBe(0));
    expect(estimatedTotal()).toBeNull();
    expect(screen.queryByText(/72 hours/)).toBeNull();
  });

  // Only pay as you go bills through a Stripe cycle; another tier's mount
  // renders nothing and reads nothing.
  it.each<ProductTier>([
    "base",
    "base_PAID",
    "__deprecated__pro",
    "enterprise",
  ])("renders nothing and fires no reads on the %s tier", (tier) => {
    mocks.productTier.mockReturnValue(tier);

    const { container } = render(<PaygCycleEstimate />);

    expect(container.childElementCount).toBe(0);
    expect(mocks.subscription).not.toHaveBeenCalled();
    expect(mocks.fetchSummary).not.toHaveBeenCalled();
  });
});

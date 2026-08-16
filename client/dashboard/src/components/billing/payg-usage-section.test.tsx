import { cleanup, render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => ({
  subscription: vi.fn(),
  summary: vi.fn(),
  creditUsage: vi.fn(),
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

vi.mock("@gram/client/react-query/getCreditUsage.js", () => ({
  useGetCreditUsage: (...args: unknown[]) => mocks.creditUsage(...args),
}));

// Page chrome isn't what's under test; render it as plain boxes so a failure
// here can only mean the section.
vi.mock("@/components/page-layout", () => {
  const Section = ({ children }: { children: ReactNode }) => <>{children}</>;
  Section.Title = ({ children }: { children: ReactNode }) => (
    <h2>{children}</h2>
  );
  Section.Description = ({ children }: { children: ReactNode }) => (
    <p>{children}</p>
  );
  Section.Body = ({ children }: { children: ReactNode }) => <>{children}</>;
  return { Page: { Section } };
});

import { PaygUsageSection } from "./payg-usage-section";

// The subscription's anchor is read against the real clock, so it is pinned far
// enough either side of now that no run can land on the wrong side of it.
const OPENED_PERIOD_START = new Date("2020-01-01T00:00:00.000Z");
const UNOPENED_PERIOD_START = new Date("2999-01-01T00:00:00.000Z");

// The dates the summary itself reports, which are what gets rendered. Midday
// UTC so a formatted day can't slide either side of the date line in whichever
// time zone the tests happen to run in.
const PERIOD_START = new Date("2026-08-01T12:00:00.000Z");
const PERIOD_END = new Date("2026-09-01T12:00:00.000Z");

type Summary = {
  periodStart: Date;
  periodEnd: Date;
  tumTokens: number | bigint;
  tumUnitPriceUsd: string;
  tumCostUsd: string;
  chatSpendUsd: string;
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
    chatSpendUsd: "4.10",
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

/** A subscription Stripe is billing, on a period that has already opened. */
function billingSubscription(status = "active") {
  stripeSubscription({ status, currentPeriodStart: OPENED_PERIOD_START });
}

type SummaryQueryState = { data?: Summary | undefined; isError?: boolean };

function summaryQuery({ data, isError = false }: SummaryQueryState = {}) {
  mocks.summary.mockReturnValue({ data, isError });
}

type CreditUsage = { creditsUsed: number; monthlyCredits: number };

const DEFAULT_CREDIT_USAGE: CreditUsage = {
  creditsUsed: 10,
  monthlyCredits: 100,
};

function creditUsageQuery(data: CreditUsage = DEFAULT_CREDIT_USAGE) {
  mocks.creditUsage.mockReturnValue({ data });
}

/** The options the estimate passed to its generated query hook. */
function summaryOptions(): { enabled?: boolean; throwOnError?: boolean } {
  const call = mocks.summary.mock.calls[0] as unknown[] | undefined;
  return (call?.[2] ?? {}) as { enabled?: boolean; throwOnError?: boolean };
}

const estimatedTotal = () => screen.queryByText("$4.29");
const meter = () => screen.queryByRole("progressbar");

describe("PaygUsageSection", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    billingSubscription();
    summaryQuery({ data: summaryData() });
    creditUsageQuery();
  });

  afterEach(cleanup);

  it("renders the cycle's billable usage and estimated invoice", () => {
    render(<PaygUsageSection />);

    expect(screen.getByText("1,234,567")).toBeTruthy();
    expect(screen.getByText("$4.10")).toBeTruthy();
    expect(estimatedTotal()).not.toBeNull();
    expect(
      screen.getByText("$0.19 at a flat $0.00000015 per token."),
    ).toBeTruthy();
  });

  it("names the cycle's own period dates", () => {
    render(<PaygUsageSection />);

    expect(
      screen.getByText(/August 1, 2026 to September 1, 2026/),
    ).toBeTruthy();
  });

  // The cutoff is the whole point of the chat figure: today's chat is not in it.
  it("names the recorded-through cutoff on chat spend", () => {
    render(<PaygUsageSection />);

    expect(
      screen.getByText(/Completed days through August 15, 2026/),
    ).toBeTruthy();
  });

  it("says so when no completed day of chat spend has landed yet", () => {
    summaryQuery({ data: summaryData({ recordedThrough: undefined }) });

    render(<PaygUsageSection />);

    expect(
      screen.getByText(/No completed day of chat spend has been recorded/),
    ).toBeTruthy();
  });

  // An invoice that finalizes days after the cycle closes must not be presented
  // as the final number.
  it("warns that the invoice finalizes up to 72 hours after the cycle", () => {
    render(<PaygUsageSection />);

    expect(
      screen.getByText(/up to 72 hours after the cycle ends/),
    ).toBeTruthy();
  });

  // The total stops at the same cutoff its chat component does, so it has to
  // say where it stops rather than reading as a complete cycle-to-date figure.
  it("carries the recorded-through cutoff onto the estimated total", () => {
    render(<PaygUsageSection />);

    expect(
      screen.getByText(/Chat spend counted through August 15, 2026/),
    ).toBeTruthy();
  });

  // int64 token counts outrun a double, so the count has to survive as digits.
  it("renders a token count beyond double precision exactly", () => {
    summaryQuery({ data: summaryData({ tumTokens: 9007199254740993n }) });

    render(<PaygUsageSection />);

    expect(screen.getByText("9,007,199,254,740,993")).toBeTruthy();
  });

  it("renders exact decimal money without rounding it to cents", () => {
    summaryQuery({
      data: summaryData({
        tumCostUsd: "1234.5678",
        estimatedTotalUsd: "1238.6678",
      }),
    });

    render(<PaygUsageSection />);

    expect(screen.getByText("$1,238.6678")).toBeTruthy();
    expect(screen.getByText(/\$1,234\.5678 at a flat/)).toBeTruthy();
  });

  describe("query conditioning", () => {
    it("enables the estimate once the period has opened", () => {
      render(<PaygUsageSection />);

      expect(summaryOptions().enabled).toBe(true);
    });

    // The billing page must survive a failing estimate, so the read opts out of
    // the shared error boundary.
    it("keeps the estimate's failures off the app error boundary", () => {
      render(<PaygUsageSection />);

      expect(summaryOptions().throwOnError).toBe(false);
    });

    it.each<[string, string]>([
      ["a trialing subscription", "trialing"],
      ["a canceled subscription", "canceled"],
      ["an incomplete subscription", "incomplete"],
    ])("never fires the estimate for %s", (_label, status) => {
      billingSubscription(status);
      summaryQuery();

      render(<PaygUsageSection />);

      expect(summaryOptions().enabled).toBe(false);
      expect(estimatedTotal()).toBeNull();
    });

    it("never fires the estimate before the period anchor", () => {
      stripeSubscription({
        status: "active",
        currentPeriodStart: UNOPENED_PERIOD_START,
      });
      summaryQuery();

      render(<PaygUsageSection />);

      expect(summaryOptions().enabled).toBe(false);
      expect(estimatedTotal()).toBeNull();
    });

    it("never fires the estimate while the subscription is unknown", () => {
      stripeSubscription(undefined);
      summaryQuery();

      render(<PaygUsageSection />);

      expect(summaryOptions().enabled).toBe(false);
    });
  });

  // A 404, a conflict, or an outage all leave the page without an estimate
  // rather than with a failure notice beside the plan's real state.
  it("shows nothing when the estimate can't be loaded", () => {
    summaryQuery({ isError: true });

    render(<PaygUsageSection />);

    expect(estimatedTotal()).toBeNull();
    expect(screen.queryByText(/72 hours/)).toBeNull();
  });

  describe("chat spend cap meter", () => {
    // The cap runs on the calendar month while the invoice runs on the Stripe
    // cycle; the two windows overlap without matching, so the copy has to say
    // which one this figure belongs to.
    it("labels the cap meter as the calendar month", () => {
      render(<PaygUsageSection />);

      expect(screen.getByText("Chat spend this calendar month")).toBeTruthy();
      expect(screen.getByText(/resets on the first of the month/)).toBeTruthy();
    });

    it("says the calendar-month spend is not part of the estimate", () => {
      render(<PaygUsageSection />);

      expect(screen.getByText(/isn't part of the estimate/)).toBeTruthy();
    });

    // The estimate's chat figure and the meter's figure come from different
    // windows and different endpoints — the meter must never feed the invoice.
    it("keeps the meter's spend out of the estimate's figures", () => {
      creditUsageQuery({ creditsUsed: 61, monthlyCredits: 100 });

      render(<PaygUsageSection />);

      expect(screen.getByText("$61.00 of $100.00")).toBeTruthy();
      expect(estimatedTotal()).not.toBeNull();
      expect(screen.getByText("$4.10")).toBeTruthy();
    });

    it("renders the meter for a trialing organization with no estimate", () => {
      billingSubscription("trialing");
      summaryQuery();

      render(<PaygUsageSection />);

      expect(meter()).not.toBeNull();
      expect(estimatedTotal()).toBeNull();
    });

    it.each<[number, number]>([
      [10, 10],
      [50, 50],
      [90, 90],
      [100, 100],
      // Overage fills the bar rather than overflowing it.
      [250, 100],
    ])("reports $%s of a $100 cap as %s%% filled", (used, expected) => {
      creditUsageQuery({ creditsUsed: used, monthlyCredits: 100 });

      render(<PaygUsageSection />);

      expect(meter()?.getAttribute("aria-valuenow")).toBe(String(expected));
    });

    it.each<[string, number, RegExp | null]>([
      ["under half", 40, null],
      ["over half", 50, /over half of this month's cap/],
      ["over three quarters", 80, /over 75% of this month's cap/],
      ["near the cap", 95, /over 90% of this month's cap/],
      ["at the cap", 100, /cap is reached/],
    ])("notes spend %s", (_label, used, note) => {
      creditUsageQuery({ creditsUsed: used, monthlyCredits: 100 });

      render(<PaygUsageSection />);

      if (note === null) {
        expect(screen.queryByText(/this month's cap\./)).toBeNull();
      } else {
        expect(screen.getByText(note)).toBeTruthy();
      }
    });

    // Without a cap the spend has nothing to be a proportion of, and a
    // full-width bar would read as a limit that was reached.
    it("draws no meter without a monthly cap", () => {
      creditUsageQuery({ creditsUsed: 10, monthlyCredits: 0 });

      render(<PaygUsageSection />);

      expect(meter()).toBeNull();
    });
  });
});

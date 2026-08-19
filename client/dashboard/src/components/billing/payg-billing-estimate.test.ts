import { describe, expect, it } from "vitest";
import {
  canEstimatePaygInvoice,
  formatExactUsd,
  formatRecordedThrough,
  formatTokenCount,
} from "./payg-billing-estimate";
import type { StripeSubscriptionLike } from "./payg-plan-state";

const PERIOD_START = new Date("2026-08-01T14:00:00.000Z");

function subscription(
  overrides: Partial<StripeSubscriptionLike> = {},
): StripeSubscriptionLike {
  return { status: "active", currentPeriodStart: PERIOD_START, ...overrides };
}

describe("canEstimatePaygInvoice", () => {
  it.each(["active", "past_due"])(
    "estimates a %s subscription once the period has opened",
    (status) => {
      const now = new Date(PERIOD_START.getTime() + 1000);

      expect(canEstimatePaygInvoice(subscription({ status }), now)).toBe(true);
    },
  );

  it("estimates from the exact instant the period opens", () => {
    expect(canEstimatePaygInvoice(subscription(), PERIOD_START)).toBe(true);
  });

  // A cycle that opens at 14:00 UTC has nothing to total up at 13:59, and the
  // endpoint refuses it — so the request must not be made.
  it("suppresses the estimate before the exact period anchor", () => {
    const now = new Date(PERIOD_START.getTime() - 1);

    expect(canEstimatePaygInvoice(subscription(), now)).toBe(false);
  });

  // The anchor's own calendar day is not the anchor: earlier that day there is
  // still no billable period.
  it("suppresses the estimate earlier on the anchor's calendar day", () => {
    const now = new Date("2026-08-01T09:00:00.000Z");

    expect(canEstimatePaygInvoice(subscription(), now)).toBe(false);
  });

  // A trial has service but no bill, so there is nothing to estimate.
  it("suppresses the estimate while Stripe is trialing", () => {
    const now = new Date("2026-08-15T00:00:00.000Z");

    expect(
      canEstimatePaygInvoice(subscription({ status: "trialing" }), now),
    ).toBe(false);
  });

  it.each(["canceled", "unpaid", "incomplete", "incomplete_expired", "paused"])(
    "suppresses the estimate for a %s subscription",
    (status) => {
      const now = new Date("2026-08-15T00:00:00.000Z");

      expect(canEstimatePaygInvoice(subscription({ status }), now)).toBe(false);
    },
  );

  it("suppresses the estimate with no subscription at all", () => {
    expect(canEstimatePaygInvoice(undefined, new Date())).toBe(false);
  });

  it.each<[string, Date | null | undefined]>([
    ["missing", undefined],
    ["null", null],
    ["malformed", new Date("nonsense")],
  ])("suppresses the estimate on a %s period anchor", (_label, start) => {
    const now = new Date("2026-08-15T00:00:00.000Z");

    expect(
      canEstimatePaygInvoice(subscription({ currentPeriodStart: start }), now),
    ).toBe(false);
  });
});

describe("formatExactUsd", () => {
  it.each([
    ["0", "$0.00"],
    ["12", "$12.00"],
    ["12.5", "$12.50"],
    ["12.50", "$12.50"],
    // Trailing zeros below cents are noise; the cents themselves are not.
    ["12.5000", "$12.50"],
    ["0.10", "$0.10"],
    ["1234.56", "$1,234.56"],
    ["1234567.891", "$1,234,567.891"],
    // A per-token unit price carries more precision than cents, and truncating
    // it to "$0.00" would misprice the whole line.
    ["0.00000015", "$0.00000015"],
    ["0.000000000001", "$0.000000000001"],
    ["+7.25", "$7.25"],
    ["-3.5", "-$3.50"],
    ["007.5", "$7.50"],
    ["0000", "$0.00"],
    // A signed zero is zero — "-$0.00" reads as a refund that isn't one.
    ["-0.00", "$0.00"],
    ["-0.000000", "$0.00"],
    ["3.", "$3.00"],
  ])("formats %s exactly as %s", (input, expected) => {
    expect(formatExactUsd(input)).toBe(expected);
  });

  // An amount larger than a double holds must survive intact: it is grouped
  // from its own digits, never parsed.
  it("keeps an amount beyond double precision exact", () => {
    expect(formatExactUsd("9007199254740993.01")).toBe(
      "$9,007,199,254,740,993.01",
    );
  });

  it.each([
    ["", "empty"],
    ["  ", "blank"],
    ["1e3", "exponent"],
    ["$12.00", "already formatted"],
    ["12,00", "comma"],
    ["abc", "letters"],
    ["1.2.3", "two points"],
    [".5", "no whole part"],
  ])("refuses %s (%s)", (input) => {
    expect(formatExactUsd(input)).toBeNull();
  });

  it.each([undefined, null])("refuses a %s amount", (input) => {
    expect(formatExactUsd(input)).toBeNull();
  });
});

describe("formatTokenCount", () => {
  it.each([
    [0, "0"],
    [999, "999"],
    [1000, "1,000"],
    [1234567, "1,234,567"],
  ])("groups the number %s as %s", (input, expected) => {
    expect(formatTokenCount(input)).toBe(expected);
  });

  // An int64 token count outruns a double, so the bigint path has to group from
  // the digits rather than through a number.
  it("groups a bigint beyond double precision exactly", () => {
    expect(formatTokenCount(9007199254740993n)).toBe("9,007,199,254,740,993");
  });

  it("groups a bigint and the equivalent number identically", () => {
    expect(formatTokenCount(1234567n)).toBe(formatTokenCount(1234567));
  });

  it.each<[string, number]>([
    ["fractional", 1.5],
    ["infinite", Number.POSITIVE_INFINITY],
    ["NaN", Number.NaN],
  ])("refuses a %s count", (_label, input) => {
    expect(formatTokenCount(input)).toBeNull();
  });

  it.each([undefined, null])("refuses a %s count", (input) => {
    expect(formatTokenCount(input)).toBeNull();
  });
});

describe("formatRecordedThrough", () => {
  it("reads the cutoff as a UTC calendar day", () => {
    expect(formatRecordedThrough("2026-08-15")).toBe("August 15, 2026");
  });

  // The generated model hands over an RFCDate, not a string.
  it("accepts anything that stringifies to the date", () => {
    expect(formatRecordedThrough({ toString: () => "2026-01-01" })).toBe(
      "January 1, 2026",
    );
  });

  it.each([
    ["2026-02-31", "a day that doesn't exist"],
    ["2026-13-01", "a month that doesn't exist"],
    ["2026-8-5", "unpadded parts"],
    ["2026-08-15T00:00:00Z", "a timestamp"],
    ["", "empty"],
  ])("refuses %s (%s)", (input) => {
    expect(formatRecordedThrough(input)).toBeNull();
  });

  it.each([undefined, null])("refuses a %s cutoff", (input) => {
    expect(formatRecordedThrough(input)).toBeNull();
  });
});

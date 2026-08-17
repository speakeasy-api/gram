import {
  formatBillingDate,
  isStripeBilling,
  type StripeSubscriptionLike,
} from "@/components/billing/payg-plan-state";
import { isValidDate } from "@/lib/trial-status";

/**
 * Whether this organization has a pay-as-you-go bill to estimate yet.
 *
 * The estimate describes the live paid Stripe service period, so it needs both
 * halves of that to be true: Stripe is billing (a trial has service but no
 * bill), and the period it would report on has actually opened. Before the
 * anchor there is no elapsed cycle to total up, and the endpoint says so with a
 * conflict — so this is what keeps the request from being made at all.
 *
 * Fails closed by construction: an absent subscription, a trial, every ended or
 * never-started status, and a missing or malformed anchor all answer false.
 */
export function canEstimatePaygInvoice(
  subscription: StripeSubscriptionLike | undefined,
  now: Date,
): boolean {
  if (subscription === undefined) return false;
  if (!isStripeBilling(subscription)) return false;

  const start = subscription.currentPeriodStart;
  // The exact anchor, not the calendar day it falls on: a cycle that opens at
  // 14:00 UTC has no billable period until 14:00 UTC.
  return isValidDate(start) && now.getTime() >= start.getTime();
}

// A signed decimal with no exponent — the shape a Postgres numeric serializes
// to. Anything else (an exponent, a stray character, an empty string) is not
// something this can render exactly, so it is refused rather than guessed at.
const EXACT_DECIMAL = /^([+-]?)(\d+)(?:\.(\d*))?$/;

// Digit grouping done on the string. Money that arrives as an exact decimal
// and token counts at int64 scale both outrun what a JS number holds, so
// neither is allowed to become one on its way to the screen.
function groupDigits(digits: string): string {
  return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ",");
}

/**
 * An exact decimal USD amount as US currency prose, or null when the input
 * isn't an exact decimal — callers render a dash rather than "$NaN" beside a
 * figure a customer is about to be invoiced for.
 *
 * Every step is a string operation. The amounts here are the ones the invoice
 * is built from, and a per-token unit price carries far more precision than a
 * double preserves, so nothing is parsed, summed, or rounded: trailing zeros
 * below two decimal places are dropped, the rest of the fraction is kept
 * exactly as the server sent it.
 */
export function formatExactUsd(
  amount: string | null | undefined,
): string | null {
  if (typeof amount !== "string") return null;

  const match = EXACT_DECIMAL.exec(amount.trim());
  if (match === null) return null;

  const [, sign = "", whole = "", fraction = ""] = match;
  // "$0.00" rather than "$0.0000" for a whole amount, and "$0.00000015" intact
  // for a unit price: pad up to cents, never truncate past them.
  const cents = fraction.replace(/0+$/, "").padEnd(2, "0");
  const dollars = groupDigits(whole.replace(/^0+(?=\d)/, ""));

  // A signed zero is still zero, and "-$0.00" reads as a refund that isn't one.
  const zero = /^0*$/.test(whole) && /^0*$/.test(fraction);
  return `${sign === "-" && !zero ? "-" : ""}$${dollars}.${cents}`;
}

/**
 * A token count as grouped digits, or null when the count isn't a whole number.
 *
 * Tokens under management are an int64. A bigint is grouped from its own digits
 * so nothing is lost; a number is routed through the same path rather than
 * through `toLocaleString`, so the two can't format the same count differently.
 */
export function formatTokenCount(
  tokens: number | bigint | null | undefined,
): string | null {
  if (typeof tokens === "bigint") return groupDigits(tokens.toString());
  if (typeof tokens !== "number" || !Number.isInteger(tokens)) return null;
  return groupDigits(BigInt(tokens).toString());
}

const RECORDED_THROUGH = /^(\d{4})-(\d{2})-(\d{2})$/;

/**
 * The `YYYY-MM-DD` spend cutoff as US English prose, or null when it is absent
 * or malformed.
 *
 * Read as a UTC calendar day rather than handed to `new Date(...)`, whose
 * date-only parse the caller's zone would shift by a day either side.
 */
export function formatRecordedThrough(
  value: { toString: () => string } | null | undefined,
): string | null {
  if (value === null || value === undefined) return null;

  const match = RECORDED_THROUGH.exec(value.toString());
  if (match === null) return null;

  const [, year = "", month = "", day = ""] = match;
  const date = new Date(
    Date.UTC(Number(year), Number(month) - 1, Number(day), 12),
  );
  // Date.UTC rolls a "2026-02-31" forward instead of rejecting it, and maps a
  // two-digit year into the 1900s, so the parts have to survive the round trip.
  if (date.getUTCFullYear() !== Number(year)) return null;
  if (date.getUTCMonth() !== Number(month) - 1) return null;
  if (date.getUTCDate() !== Number(day)) return null;

  return formatBillingDate(date);
}

/**
 * The highest inference-cap threshold this month's spend has crossed.
 *
 * The same ladder the alert emails walk, read the same way — the percentage is
 * truncated before it is compared — so the meter can't show a band the customer
 * hasn't been emailed about, or stay quiet through one they have. 0 means the
 * spend is below every threshold.
 */
export type SpendCapThreshold = 0 | 50 | 75 | 90 | 100;

export function crossedSpendCapThreshold(
  used: number,
  cap: number,
): SpendCapThreshold {
  if (!Number.isFinite(used) || !Number.isFinite(cap) || cap <= 0) return 0;

  const percent = Math.floor((used / cap) * 100);
  if (percent < 50) return 0;
  if (percent < 75) return 50;
  if (percent < 90) return 75;
  if (percent < 100) return 90;
  return 100;
}

/** How much of the cap meter is filled, clamped so overage can't overflow it. */
export function spendCapFillPercent(used: number, cap: number): number {
  if (!Number.isFinite(used) || !Number.isFinite(cap) || cap <= 0) return 0;
  return Math.min(100, Math.max(0, (used / cap) * 100));
}
